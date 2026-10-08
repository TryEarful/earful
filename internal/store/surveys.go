package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store/db"
)

// Surveys is the data-access surface for survey building (M3). It owns
// the transactions that make the product's guarantees true: a survey and
// its draft are created together, every draft save appends a revision,
// and publishing freezes a whole version atomically.
type Surveys struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewSurveys(pool *pgxpool.Pool) *Surveys {
	return &Surveys{pool: pool, q: db.New(pool)}
}

var (
	// ErrNotFound covers both "no such survey" and "not yours": callers
	// must not be able to tell the difference, or the id space becomes a
	// membership oracle for other workspaces.
	ErrNotFound = errors.New("store: survey not found")
	// ErrNothingToPublish guards the double-click: republishing an
	// unchanged draft would add a version indistinguishable from the last
	// one, polluting the history results aggregate over (ADR-0001).
	ErrNothingToPublish = errors.New("store: draft matches the published version")
)

// Survey is one survey with everything the creator UI needs to describe
// it, including its derived Status.
type Survey struct {
	ID            uuid.UUID
	WorkspaceID   uuid.UUID
	Title         string
	IsAnonymous   bool
	CloseAt       *time.Time
	ClosedAt      *time.Time
	CreatedAt     time.Time
	LatestVersion int // 0 when never published
	QuestionCount int
	// Origin is OriginCreator or OriginStarter: how the survey came to
	// exist, never how it behaves.
	Origin string
}

// The origins a survey can have (migration 00017).
const (
	OriginCreator = "creator"
	OriginStarter = "starter"
)

// State reduces a survey to the facts Status is derived from.
func (s Survey) State() domain.SurveyState {
	return domain.SurveyState{
		HasPublishedVersion: s.LatestVersion > 0,
		CloseAt:             s.CloseAt,
		ClosedAt:            s.ClosedAt,
	}
}

func (s Survey) StatusAt(now time.Time) domain.Status { return s.State().StatusAt(now) }

// Create makes a survey and its (empty) draft in one transaction, so a
// survey never exists without somewhere to write its structure.
func (s *Surveys) Create(ctx context.Context, workspaceID, userID uuid.UUID, title string, isAnonymous bool, closeAt *time.Time) (Survey, error) {
	if err := domain.ValidateTitle(title); err != nil {
		return Survey{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Survey{}, fmt.Errorf("store: begin create survey: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	row, err := createSurvey(ctx, s.q.WithTx(tx), workspaceID, userID, title, isAnonymous, closeAt, OriginCreator)
	if err != nil {
		return Survey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Survey{}, fmt.Errorf("store: commit create survey: %w", err)
	}
	return surveyFromRow(row, 0, 0), nil
}

// createSurvey inserts a survey and its empty draft through q. The caller
// owns the transaction q is bound to: the two rows belong together, and
// only the caller knows what else must land with them.
func createSurvey(ctx context.Context, q *db.Queries, workspaceID, userID uuid.UUID, title string, isAnonymous bool, closeAt *time.Time, origin string) (db.Survey, error) {
	row, err := q.CreateSurvey(ctx, db.CreateSurveyParams{
		WorkspaceID: workspaceID,
		Title:       title,
		IsAnonymous: isAnonymous,
		CloseAt:     closeAt,
		CreatedBy:   userID,
		Origin:      origin,
	})
	if err != nil {
		return db.Survey{}, fmt.Errorf("store: create survey: %w", err)
	}
	empty, err := domain.Draft{}.Encode()
	if err != nil {
		return db.Survey{}, err
	}
	if _, err := q.CreateDraft(ctx, db.CreateDraftParams{
		SurveyID:  row.ID,
		Structure: empty,
		UpdatedBy: uuid.NullUUID{UUID: userID, Valid: true},
	}); err != nil {
		return db.Survey{}, fmt.Errorf("store: create draft: %w", err)
	}
	return row, nil
}

// Get loads one survey, scoped to the workspace.
func (s *Surveys) Get(ctx context.Context, workspaceID, surveyID uuid.UUID) (Survey, error) {
	row, err := s.q.GetSurveyForWorkspace(ctx, db.GetSurveyForWorkspaceParams{
		ID: surveyID, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Survey{}, ErrNotFound
	}
	if err != nil {
		return Survey{}, fmt.Errorf("store: get survey: %w", err)
	}

	survey := surveyFromRow(row, 0, 0)
	latest, err := s.q.GetLatestVersion(ctx, surveyID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Never published: stays a Draft.
	case err != nil:
		return Survey{}, fmt.Errorf("store: get latest version: %w", err)
	default:
		survey.LatestVersion = int(latest.Number)
		questions, err := s.q.ListQuestionsForVersion(ctx, latest.ID)
		if err != nil {
			return Survey{}, fmt.Errorf("store: count questions: %w", err)
		}
		survey.QuestionCount = len(questions)
	}
	return survey, nil
}

// List returns the workspace's surveys, newest first.
func (s *Surveys) List(ctx context.Context, workspaceID uuid.UUID) ([]Survey, error) {
	rows, err := s.q.ListSurveysForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("store: list surveys: %w", err)
	}
	out := make([]Survey, 0, len(rows))
	for _, r := range rows {
		out = append(out, surveyFromRow(db.Survey{
			ID: r.ID, WorkspaceID: r.WorkspaceID, Title: r.Title,
			IsAnonymous: r.IsAnonymous, CloseAt: r.CloseAt, ClosedAt: r.ClosedAt,
			CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, DeletedAt: r.DeletedAt,
			Origin: r.Origin,
		}, int(r.LatestVersion), int(r.LatestQuestionCount)))
	}
	return out, nil
}

// Draft loads the working copy for a survey.
func (s *Surveys) Draft(ctx context.Context, surveyID uuid.UUID) (domain.Draft, uuid.UUID, error) {
	return draftWith(ctx, s.q, surveyID)
}

// draftWith loads the working copy through q, which may be inside a
// transaction.
func draftWith(ctx context.Context, q *db.Queries, surveyID uuid.UUID) (domain.Draft, uuid.UUID, error) {
	row, err := q.GetDraftForSurvey(ctx, surveyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Draft{}, uuid.Nil, ErrNotFound
	}
	if err != nil {
		return domain.Draft{}, uuid.Nil, fmt.Errorf("store: get draft: %w", err)
	}
	draft, err := domain.ParseDraft(row.Structure)
	if err != nil {
		return domain.Draft{}, uuid.Nil, err
	}
	return draft, row.ID, nil
}

// SaveDraft writes the working copy and appends a Draft Revision in the
// same transaction — the two cannot diverge, so the audit trail is
// complete by construction (M3-T2).
func (s *Surveys) SaveDraft(ctx context.Context, surveyID, userID uuid.UUID, draft domain.Draft, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin save draft: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	if err := saveDraft(ctx, s.q.WithTx(tx), surveyID, userID, draft, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// saveDraft writes the working copy and its revision through q, inside
// the caller's transaction.
func saveDraft(ctx context.Context, q *db.Queries, surveyID, userID uuid.UUID, draft domain.Draft, now time.Time) error {
	encoded, err := draft.Encode()
	if err != nil {
		return err
	}
	updated, err := q.UpdateDraftStructure(ctx, db.UpdateDraftStructureParams{
		SurveyID:  surveyID,
		Structure: encoded,
		UpdatedBy: uuid.NullUUID{UUID: userID, Valid: true},
		UpdatedAt: now,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: update draft: %w", err)
	}
	if err := q.CreateDraftRevision(ctx, db.CreateDraftRevisionParams{
		DraftID:   updated.ID,
		Structure: encoded,
		SavedBy:   uuid.NullUUID{UUID: userID, Valid: true},
		SavedAt:   now,
	}); err != nil {
		return fmt.Errorf("store: append revision: %w", err)
	}
	return nil
}

// Version is a published snapshot as the audit log lists it.
type Version struct {
	ID          uuid.UUID
	Number      int
	PublishedAt time.Time
	PublishedBy string
	// Thanks is the thank you page this version was published with.
	Thanks domain.ThankYou
	// Style is the style this version was published with.
	Style domain.Style
}

// LatestStyle is the style of a survey's latest version, and the same
// style worded in each language that version was published with.
type LatestStyle struct {
	Style     domain.Style
	Localized map[string]domain.Style
}

// In returns the style as somebody reading lang sees it: translated
// where the version carries a translation, as written where it does not.
func (l LatestStyle) In(lang string) domain.Style {
	if style, ok := l.Localized[lang]; ok {
		return style
	}
	return l.Style
}

// Revision is one saved draft state as the audit log lists it.
type Revision struct {
	ID            uuid.UUID
	SavedAt       time.Time
	SavedBy       string
	QuestionCount int
}

// Publish freezes the current draft into a new immutable Survey Version.
// Everything happens in one transaction: identities, the version row and
// every question land together or not at all, so no half-published
// version can ever be served.
//
// The survey is held first and its account's style read under a hold of
// its own (ADR-0023), and only then is the draft checked and compared:
// the account's style cannot change between the checks that pass it and
// the version that freezes it.
func (s *Surveys) Publish(ctx context.Context, workspaceID, surveyID, userID uuid.UUID, now time.Time) (Version, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("store: begin publish: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	qtx := s.q.WithTx(tx)

	_, account, err := holdForStyle(ctx, qtx, surveyID)
	if err != nil {
		return Version{}, err
	}
	draft, _, err := draftWith(ctx, qtx, surveyID)
	if err != nil {
		return Version{}, err
	}
	if err := draft.ValidateForPublish(); err != nil {
		return Version{}, err
	}
	// Story 23: nothing goes out in a creator's name that they have not
	// read. A language with an unreviewed or stale translation blocks
	// the publish rather than shipping a machine's guess, and so does one
	// the account's style, where the survey shows it, is not read in.
	if err := draft.ReadyToPublish(account); err != nil {
		return Version{}, err
	}
	if err := draft.ResolvedStyle(account.Style).Validate(); err != nil {
		return Version{}, err
	}

	// Refuse a republish that would change nothing.
	unpublished, err := unpublishedChanges(ctx, qtx, surveyID, draft, account)
	if err != nil {
		return Version{}, err
	}
	if !unpublished.Changed {
		return Version{}, ErrNothingToPublish
	}

	version, err := publishResolved(ctx, qtx, surveyID, userID, draft, account, now)
	if err != nil {
		return Version{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, fmt.Errorf("store: commit publish: %w", err)
	}
	return Version{ID: version.ID, Number: int(version.Number), PublishedAt: version.PublishedAt}, nil
}

// publishDraft freezes draft as the survey's next version through q,
// inside the caller's transaction, resolved against its account's style
// as it stands. It checks nothing about the draft: whether it may be
// published is the caller's to decide before calling.
func publishDraft(ctx context.Context, qtx *db.Queries, surveyID, userID uuid.UUID, draft domain.Draft, now time.Time) (db.SurveyVersion, error) {
	_, account, err := holdForStyle(ctx, qtx, surveyID)
	if err != nil {
		return db.SurveyVersion{}, err
	}
	return publishResolved(ctx, qtx, surveyID, userID, draft, account, now)
}

// publishResolved freezes draft as the survey's next version, its style
// resolved against account, through q, inside a transaction that holds
// the survey and the account's style (holdForStyle).
func publishResolved(ctx context.Context, qtx *db.Queries, surveyID, userID uuid.UUID, draft domain.Draft, account domain.WorkspaceStyle, now time.Time) (db.SurveyVersion, error) {
	// The survey is held until the version is in, so that no picture its
	// style shows is removed in between. The account's pictures it shows
	// are copied into the survey first (ADR-0023), and a style that refers
	// to a picture no longer stored is refused: once published, a version
	// would show the broken picture for good (ADR-0001, ADR-0018).
	resolved := draft.ResolvedStyle(account.Style)
	workspaceID, err := qtx.LockSurveyForStyle(ctx, surveyID)
	if err != nil {
		return db.SurveyVersion{}, fmt.Errorf("store: hold survey: %w", err)
	}
	if err := ensureSurveyImages(ctx, qtx, workspaceID, surveyID, styleHashes(resolved), now); err != nil {
		return db.SurveyVersion{}, err
	}
	if err := checkStyleImages(ctx, qtx, surveyID, resolved); err != nil {
		return db.SurveyVersion{}, err
	}
	next, err := qtx.NextVersionNumber(ctx, surveyID)
	if err != nil {
		return db.SurveyVersion{}, fmt.Errorf("store: next version number: %w", err)
	}
	// The thank you page freezes with the questions: a respondent reads
	// the one their version was published with (ADR-0001).
	thanksMessage, thanksLabel, thanksURL, thanksLocalized, err := thanksColumns(draft)
	if err != nil {
		return db.SurveyVersion{}, err
	}
	// So does the style: the look of the page is part of what a
	// respondent was shown (ADR-0018).
	style, err := styleColumn(draft, account)
	if err != nil {
		return db.SurveyVersion{}, err
	}
	version, err := qtx.CreateVersion(ctx, db.CreateVersionParams{
		SurveyID:            surveyID,
		Number:              int32(next),
		PublishedBy:         uuid.NullUUID{UUID: userID, Valid: true},
		PublishedAt:         now,
		ThanksMessage:       thanksMessage,
		ThanksLinkLabel:     thanksLabel,
		ThanksLinkUrl:       thanksURL,
		ThanksLocalizations: thanksLocalized,
		Style:               style,
	})
	if err != nil {
		return db.SurveyVersion{}, fmt.Errorf("store: create version: %w", err)
	}

	for i, q := range draft.Questions {
		identityID, err := uuid.Parse(q.IdentityID)
		if err != nil {
			return db.SurveyVersion{}, fmt.Errorf("store: question %d has an invalid identity: %w", i+1, err)
		}
		if err := qtx.EnsureQuestionIdentity(ctx, db.EnsureQuestionIdentityParams{
			ID: identityID, SurveyID: surveyID,
		}); err != nil {
			return db.SurveyVersion{}, fmt.Errorf("store: ensure identity: %w", err)
		}
		options, err := json.Marshal(q.Options)
		if err != nil {
			return db.SurveyVersion{}, fmt.Errorf("store: encode options: %w", err)
		}
		// Scale bounds, and a number's limits, are frozen alongside the
		// wording: a later version may rescale a question, and a response
		// must be read back against the bounds it was actually shown
		// (ADR-0001).
		var scaleMin, scaleMax *int32
		if q.Type.HasBounds() {
			min, max := q.Scale()
			scaleMin, scaleMax = int32ptr(min), int32ptr(max)
		}
		question, err := qtx.CreateQuestion(ctx, db.CreateQuestionParams{
			VersionID:          version.ID,
			QuestionIdentityID: identityID,
			Type:               string(q.Type),
			Text:               q.Text,
			Options:            options,
			Required:           q.Required,
			Position:           int32(i),
			ScaleMin:           scaleMin,
			ScaleMax:           scaleMax,
			AllowOther:         q.AllowOther,
		})
		if err != nil {
			return db.SurveyVersion{}, fmt.Errorf("store: create question: %w", err)
		}

		// Localizations freeze with the question they translate: what a
		// respondent saw in their language is as immutable as what
		// anyone else saw (M11-T1).
		for _, lang := range draft.Languages() {
			translated, ok := draft.Localizations[lang].Questions[q.IdentityID]
			if !ok || translated.Text == "" {
				continue
			}
			options, err := json.Marshal(translated.Options)
			if err != nil {
				return db.SurveyVersion{}, fmt.Errorf("store: encode localized options: %w", err)
			}
			if err := qtx.CreateQuestionLocalization(ctx, db.CreateQuestionLocalizationParams{
				VersionID:  version.ID,
				QuestionID: question.ID,
				Lang:       lang,
				Text:       translated.Text,
				Options:    options,
			}); err != nil {
				return db.SurveyVersion{}, fmt.Errorf("store: create localization: %w", err)
			}
		}
	}
	return version, nil
}

// Unpublished says whether publishing a draft would make a new version,
// and why.
type Unpublished struct {
	// Changed is whether publishing would make a new version.
	Changed bool
	// AccountStyleOnly is whether the only change is to parts of the
	// style the survey follows from its account (ADR-0023): the creator
	// changed nothing of the survey's, and publishing applies the
	// account's style as it now stands.
	AccountStyleOnly bool
}

// HasUnpublishedChanges reports whether publishing the given draft would
// make a new version: always before the first publish, and afterwards
// only when its questions, its thank you page or its style, resolved
// against its account's, differ from the live version's. It is the
// comparison Publish refuses by, so the editor offers the button only
// where pressing it would do something.
func (s *Surveys) HasUnpublishedChanges(ctx context.Context, surveyID uuid.UUID, draft domain.Draft, account domain.WorkspaceStyle) (Unpublished, error) {
	return unpublishedChanges(ctx, s.q, surveyID, draft, account)
}

func unpublishedChanges(ctx context.Context, q *db.Queries, surveyID uuid.UUID, draft domain.Draft, account domain.WorkspaceStyle) (Unpublished, error) {
	latest, err := q.GetLatestVersion(ctx, surveyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Unpublished{Changed: true}, nil
	}
	if err != nil {
		return Unpublished{}, fmt.Errorf("store: get latest version: %w", err)
	}
	questions, err := questionsForVersion(ctx, q, latest.ID)
	if err != nil {
		return Unpublished{}, err
	}
	if !questionsEqual(questions, draft.Questions) {
		return Unpublished{Changed: true}, nil
	}
	thanks, _, err := thanksFromVersion(latest)
	if err != nil {
		return Unpublished{}, err
	}
	if thanks != draft.Thanks {
		return Unpublished{Changed: true}, nil
	}
	style, _, err := styleFromColumn(latest.Style)
	if err != nil {
		return Unpublished{}, err
	}
	differs := domain.StyleDiffers(style, draft.ResolvedStyle(account.Style))
	if differs.IsZero() {
		return Unpublished{}, nil
	}
	// The account is the only reason when every part that differs is one
	// the survey follows from it.
	followed := draft.FollowedParts()
	accountOnly := (!differs.Theme || followed.Theme) && (!differs.Header || followed.Header) &&
		(!differs.Footer || followed.Footer) && (!differs.Thanks || followed.Thanks)
	return Unpublished{Changed: true, AccountStyleOnly: accountOnly}, nil
}

// LatestStyle is the style of the most recent published version: what a
// page about the survey as a whole is drawn in, such as the page that
// says it is closed. A survey never published has none.
func (s *Surveys) LatestStyle(ctx context.Context, surveyID uuid.UUID) (LatestStyle, error) {
	latest, err := s.q.GetLatestVersion(ctx, surveyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return LatestStyle{}, nil
	}
	if err != nil {
		return LatestStyle{}, fmt.Errorf("store: get latest version: %w", err)
	}
	style, localized, err := styleFromColumn(latest.Style)
	if err != nil {
		return LatestStyle{}, err
	}
	return LatestStyle{Style: style, Localized: localized}, nil
}

// LatestQuestions returns the questions of the most recent published
// version, or nil when the survey has never been published.
func (s *Surveys) LatestQuestions(ctx context.Context, surveyID uuid.UUID) ([]domain.Question, error) {
	latest, err := s.q.GetLatestVersion(ctx, surveyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: get latest version: %w", err)
	}
	return s.QuestionsForVersion(ctx, latest.ID)
}

// QuestionsForVersion reads a frozen version back into domain form — the
// same shape the draft uses, so one renderer serves both.
func (s *Surveys) QuestionsForVersion(ctx context.Context, versionID uuid.UUID) ([]domain.Question, error) {
	return questionsForVersion(ctx, s.q, versionID)
}

// questionsForVersion is QuestionsForVersion through q, which may be
// inside a transaction.
func questionsForVersion(ctx context.Context, q *db.Queries, versionID uuid.UUID) ([]domain.Question, error) {
	rows, err := q.ListQuestionsForVersion(ctx, versionID)
	if err != nil {
		return nil, fmt.Errorf("store: list questions: %w", err)
	}
	out := make([]domain.Question, 0, len(rows))
	for _, r := range rows {
		var options []string
		if len(r.Options) > 0 {
			if err := json.Unmarshal(r.Options, &options); err != nil {
				return nil, fmt.Errorf("store: decode options: %w", err)
			}
		}
		q := domain.Question{
			IdentityID: r.QuestionIdentityID.String(),
			Type:       domain.QuestionType(r.Type),
			Text:       r.Text,
			Options:    options,
			Required:   r.Required,
			AllowOther: r.AllowOther,
		}
		// NULL for versions published before migration 00009; Scale() then
		// applies the documented fallback rather than a 0..0 scale.
		if r.ScaleMin != nil {
			q.ScaleMin = int(*r.ScaleMin)
		}
		if r.ScaleMax != nil {
			q.ScaleMax = int(*r.ScaleMax)
		}
		out = append(out, q)
	}
	return out, nil
}

// Versions lists published versions, newest first.
func (s *Surveys) Versions(ctx context.Context, surveyID uuid.UUID) ([]Version, error) {
	rows, err := s.q.ListVersions(ctx, surveyID)
	if err != nil {
		return nil, fmt.Errorf("store: list versions: %w", err)
	}
	out := make([]Version, 0, len(rows))
	for _, r := range rows {
		v := Version{ID: r.ID, Number: int(r.Number), PublishedAt: r.PublishedAt,
			Thanks: domain.ThankYou{
				Message:   deref(r.ThanksMessage),
				LinkLabel: deref(r.ThanksLinkLabel),
				LinkURL:   deref(r.ThanksLinkUrl),
			}}
		if r.PublishedByEmail != nil {
			v.PublishedBy = *r.PublishedByEmail
		}
		if v.Style, _, err = styleFromColumn(r.Style); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// Revisions lists draft saves, newest first — the other half of the audit
// log (M3-T4).
func (s *Surveys) Revisions(ctx context.Context, draftID uuid.UUID) ([]Revision, error) {
	rows, err := s.q.ListDraftRevisions(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("store: list revisions: %w", err)
	}
	out := make([]Revision, 0, len(rows))
	for _, r := range rows {
		rev := Revision{ID: r.ID, SavedAt: r.SavedAt}
		if r.SavedByEmail != nil {
			rev.SavedBy = *r.SavedByEmail
		}
		if d, err := domain.ParseDraft(r.Structure); err == nil {
			rev.QuestionCount = len(d.Questions)
		}
		out = append(out, rev)
	}
	return out, nil
}

// UpdateSettings changes the title and Close Date. Anonymity is absent by
// design: it is fixed at creation (ADR-0003) and the database enforces it.
func (s *Surveys) UpdateSettings(ctx context.Context, workspaceID, surveyID uuid.UUID, title string, closeAt *time.Time) error {
	if err := domain.ValidateTitle(title); err != nil {
		return err
	}
	return s.q.UpdateSurveySettings(ctx, db.UpdateSurveySettingsParams{
		ID: surveyID, WorkspaceID: workspaceID, Title: title, CloseAt: closeAt,
	})
}

// SetClosed closes or reopens a survey. Reopening also clears a Close Date
// that has already passed, since otherwise the survey would close again
// the instant it reopened.
func (s *Surveys) SetClosed(ctx context.Context, workspaceID, surveyID uuid.UUID, closed bool, now time.Time) error {
	if closed {
		return s.q.SetSurveyClosedAt(ctx, db.SetSurveyClosedAtParams{
			ID: surveyID, WorkspaceID: workspaceID, ClosedAt: &now,
		})
	}

	survey, err := s.Get(ctx, workspaceID, surveyID)
	if err != nil {
		return err
	}
	if err := s.q.SetSurveyClosedAt(ctx, db.SetSurveyClosedAtParams{
		ID: surveyID, WorkspaceID: workspaceID, ClosedAt: nil,
	}); err != nil {
		return err
	}
	if survey.CloseAt != nil && !now.Before(*survey.CloseAt) {
		return s.q.UpdateSurveySettings(ctx, db.UpdateSurveySettingsParams{
			ID: surveyID, WorkspaceID: workspaceID, Title: survey.Title, CloseAt: nil,
		})
	}
	return nil
}

// SoftDelete marks a survey deleted; the purge job (M8-T2) removes it for
// good after the retention window.
func (s *Surveys) SoftDelete(ctx context.Context, workspaceID, surveyID uuid.UUID, now time.Time) error {
	return s.q.SoftDeleteSurvey(ctx, db.SoftDeleteSurveyParams{
		ID: surveyID, WorkspaceID: workspaceID, DeletedAt: &now,
	})
}

func surveyFromRow(r db.Survey, latestVersion, questionCount int) Survey {
	return Survey{
		ID: r.ID, WorkspaceID: r.WorkspaceID, Title: r.Title,
		IsAnonymous: r.IsAnonymous, CloseAt: r.CloseAt, ClosedAt: r.ClosedAt,
		CreatedAt: r.CreatedAt, LatestVersion: latestVersion, QuestionCount: questionCount,
		Origin: r.Origin,
	}
}

// questionsEqual compares a published version to a draft on everything a
// respondent would notice.
func int32ptr(v int) *int32 {
	n := int32(v)
	return &n
}

func questionsEqual(a, b []domain.Question) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.IdentityID != y.IdentityID || x.Type != y.Type || x.Text != y.Text || x.Required != y.Required ||
			x.AllowOther != y.AllowOther {
			return false
		}
		// Rescaling a rating question is a real change, so it must not be
		// mistaken for "nothing to publish".
		xMin, xMax := x.Scale()
		yMin, yMax := y.Scale()
		if xMin != yMin || xMax != yMax {
			return false
		}
		if len(x.Options) != len(y.Options) {
			return false
		}
		for j := range x.Options {
			if x.Options[j] != y.Options[j] {
				return false
			}
		}
	}
	return true
}
