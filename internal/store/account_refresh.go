package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store/db"
)

// A change to an account's style reaches a published survey when it is
// next published (ADR-0023). These apply it now, on the creator's word,
// to the surveys that are open: each is published again from its live
// version, never from its draft, so that what the creator has not
// finished stays unpublished.

// StaleSurvey is an open survey whose live version shows an earlier
// version of its account's style.
type StaleSurvey struct {
	ID    uuid.UUID
	Title string
	// Hold says why the survey cannot take the change yet, and is empty
	// when it can.
	Hold Hold
	// BlockedIn names the language the account's style has to be
	// reviewed in, when Hold is HoldLanguage.
	BlockedIn string
}

// Held reports whether the survey cannot take the change yet.
func (s StaleSurvey) Held() bool { return s.Hold != "" }

// Hold is why a survey cannot take its account's style yet. Each is
// something the creator can put right, so the survey is named with it
// rather than the change refused for every survey.
type Hold string

const (
	// HoldLanguage: the account's style is not reviewed in a language
	// the survey went out in.
	HoldLanguage Hold = "language"
	// HoldPictures: the survey keeps its limit of pictures, every one
	// shown by a version, so the account's new ones do not fit.
	HoldPictures Hold = "pictures"
	// HoldPictureMissing: a picture the survey would show is stored
	// neither by the survey nor by its account.
	HoldPictureMissing Hold = "picture_missing"
)

// ErrAccountStyleChanged refuses to apply an account's style that has
// changed since the creator was asked: they are asked again about the
// style as it now is.
var ErrAccountStyleChanged = errors.New("store: the account style changed since the surveys were listed")

// StaleSurveys lists the workspace's open surveys whose live version,
// published again, would be drawn differently with account as it stands:
// every survey a change to the account has not reached yet.
func (s *Surveys) StaleSurveys(ctx context.Context, workspaceID uuid.UUID, account domain.WorkspaceStyle, now time.Time) ([]StaleSurvey, error) {
	return s.openFollowing(ctx, workspaceID, account, now, func(v domain.PublishedVersion) (bool, string) {
		r := v.RefreshAgainst(account)
		return r.Changes, r.BlockedIn
	})
}

// ActiveSurveysAffected lists the workspace's open surveys that a change
// to the account's style from before to after reaches and has not
// reached yet: those whose live version follows a part that changed, in
// the original or in a language the survey went out in. A change to a
// translation alone counts, for a survey that went out in that language.
func (s *Surveys) ActiveSurveysAffected(ctx context.Context, workspaceID uuid.UUID, before, after domain.WorkspaceStyle, now time.Time) ([]StaleSurvey, error) {
	return s.openFollowing(ctx, workspaceID, after, now, func(v domain.PublishedVersion) (bool, string) {
		if !v.ChangedBetween(before, after) {
			return false, ""
		}
		r := v.RefreshAgainst(after)
		return r.Changes, r.BlockedIn
	})
}

// openFollowing lists the workspace's open surveys whose live version
// follows a part of the account's style and is chosen by pick, with the
// language pick names as blocking it. A survey that could not keep the
// account's new pictures under its limit, or would show a picture stored
// nowhere, is named with that, so it is not offered an update that would
// be refused.
func (s *Surveys) openFollowing(ctx context.Context, workspaceID uuid.UUID, account domain.WorkspaceStyle, now time.Time, pick func(domain.PublishedVersion) (bool, string)) ([]StaleSurvey, error) {
	rows, err := s.q.ListSurveysForWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("store: list surveys: %w", err)
	}
	var out []StaleSurvey
	// The dashboard lists the newest first; the question lists them in
	// the order they were made.
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		state := domain.SurveyState{HasPublishedVersion: row.LatestVersion > 0, CloseAt: row.CloseAt, ClosedAt: row.ClosedAt}
		if state.StatusAt(now) != domain.StatusOpen {
			continue
		}
		latest, err := s.q.GetLatestVersion(ctx, row.ID)
		if err != nil {
			return nil, fmt.Errorf("store: get latest version: %w", err)
		}
		v, err := publishedStyle(ctx, s.q, latest)
		if err != nil {
			return nil, err
		}
		if v.Follows.IsZero() {
			continue
		}
		ok, blocked := pick(v)
		if !ok {
			continue
		}
		stale := StaleSurvey{ID: row.ID, Title: row.Title}
		if blocked != "" {
			stale.Hold, stale.BlockedIn = HoldLanguage, blocked
		} else {
			hashes := styleHashes(v.Draft().ResolvedStyle(account.Style))
			missing, err := picturesMissing(ctx, s.q, workspaceID, row.ID, hashes)
			if err != nil {
				return nil, err
			}
			fits, err := roomForCopies(ctx, s.q, workspaceID, row.ID, hashes)
			if err != nil {
				return nil, err
			}
			switch {
			case missing:
				stale.Hold = HoldPictureMissing
			case !fits:
				stale.Hold = HoldPictures
			}
		}
		out = append(out, stale)
	}
	return out, nil
}

// RefreshResult is what applying an account's style did.
type RefreshResult struct {
	// Updated are the titles of the surveys that went out again.
	Updated []string
	// Skipped are the surveys that could not take the change yet, each
	// with what holds it back.
	Skipped []StaleSurvey
}

// RefreshFromAccount publishes each of the named surveys again from its
// live version, with the parts it follows taken from its account's style
// as it now stands (ADR-0023). accountUpdatedAt is the account's style
// the creator was asked about, in microseconds since the epoch; a style
// changed since is refused with ErrAccountStyleChanged, so the creator is
// asked about the one that would be applied. A survey of another
// workspace refuses the whole request with ErrNotFound before anything
// is published. A survey closed since, one that follows nothing of the
// account's, one the change has already reached, or one deleted since it
// was listed is left alone. One the creator has to put something right
// for first (a language the account's style is not read in yet, or no
// room for the account's new pictures) is skipped and named, and the
// rest still go out. Each survey goes out in a transaction of its own,
// held as a publish holds it, and its draft is not touched: what the
// creator has not published stays unpublished. An account's style that
// changes partway through stops the surveys not reached yet with
// ErrAccountStyleChanged, and the result names those that went out.
func (s *Surveys) RefreshFromAccount(ctx context.Context, workspaceID uuid.UUID, surveyIDs []uuid.UUID, accountUpdatedAt int64, userID uuid.UUID, now time.Time) (RefreshResult, error) {
	for _, id := range surveyIDs {
		ours, err := s.q.SurveyBelongsToWorkspace(ctx, db.SurveyBelongsToWorkspaceParams{ID: id, WorkspaceID: workspaceID})
		if err != nil {
			return RefreshResult{}, fmt.Errorf("store: find survey: %w", err)
		}
		if !ours {
			return RefreshResult{}, ErrNotFound
		}
	}
	account, err := s.WorkspaceStyle(ctx, workspaceID)
	if err != nil {
		return RefreshResult{}, err
	}
	if account.UpdatedAt.UnixMicro() != accountUpdatedAt {
		return RefreshResult{}, ErrAccountStyleChanged
	}
	var result RefreshResult
	for _, id := range surveyIDs {
		outcome, err := s.refreshOne(ctx, workspaceID, id, accountUpdatedAt, userID, now)
		if err != nil {
			return result, err
		}
		switch {
		case outcome.updated:
			result.Updated = append(result.Updated, outcome.Title)
		case outcome.Held():
			result.Skipped = append(result.Skipped, outcome.StaleSurvey)
		}
	}
	return result, nil
}

// refreshOutcome is what refreshOne did with one survey.
type refreshOutcome struct {
	StaleSurvey
	updated bool
}

// refreshOne publishes one survey again from its live version, as
// RefreshFromAccount describes, and says whether it did, or what held it
// back. A survey gone since it was listed is left alone, with no error.
func (s *Surveys) refreshOne(ctx context.Context, workspaceID, surveyID uuid.UUID, accountUpdatedAt int64, userID uuid.UUID, now time.Time) (refreshOutcome, error) {
	out := refreshOutcome{StaleSurvey: StaleSurvey{ID: surveyID}}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, fmt.Errorf("store: begin refresh: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op
	qtx := s.q.WithTx(tx)

	// The survey first, then the account's style, as a publish holds them.
	_, account, err := holdForStyle(ctx, qtx, surveyID)
	if errors.Is(err, ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if account.UpdatedAt.UnixMicro() != accountUpdatedAt {
		return out, ErrAccountStyleChanged
	}
	survey, err := qtx.GetSurveyForWorkspace(ctx, db.GetSurveyForWorkspaceParams{ID: surveyID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("store: get survey: %w", err)
	}
	out.Title = survey.Title
	latest, err := qtx.GetLatestVersion(ctx, surveyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("store: get latest version: %w", err)
	}
	state := domain.SurveyState{HasPublishedVersion: true, CloseAt: survey.CloseAt, ClosedAt: survey.ClosedAt}
	if state.StatusAt(now) != domain.StatusOpen {
		return out, nil
	}
	v, err := publishedVersion(ctx, qtx, latest)
	if err != nil {
		return out, err
	}
	refresh := v.RefreshAgainst(account)
	if !refresh.Changes {
		return out, nil
	}
	if refresh.BlockedIn != "" {
		out.Hold, out.BlockedIn = HoldLanguage, refresh.BlockedIn
		return out, nil
	}
	draft := v.Draft()
	if err := draft.ReadyToPublish(account); err != nil {
		return out, err
	}
	if err := draft.ResolvedStyle(account.Style).Validate(); err != nil {
		return out, err
	}
	_, err = publishResolved(ctx, qtx, surveyID, userID, draft, account, now)
	var full ImageLimitError
	switch {
	case errors.As(err, &full):
		// The rollback leaves any picture removed to make room in place.
		out.Hold = HoldPictures
		return out, nil
	case errors.Is(err, ErrStyleImageMissing):
		out.Hold = HoldPictureMissing
		return out, nil
	case err != nil:
		return out, err
	}
	if err := tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("store: commit refresh: %w", err)
	}
	out.updated = true
	return out, nil
}

// publishedStyle reads what a version froze of its style, and the
// languages it went out in: enough to tell whether its account's style
// would draw it differently, without its questions.
func publishedStyle(ctx context.Context, q *db.Queries, v db.SurveyVersion) (domain.PublishedVersion, error) {
	var out domain.PublishedVersion
	if len(v.Style) > 0 {
		var frozen frozenStyle
		if err := json.Unmarshal(v.Style, &frozen); err != nil {
			return out, fmt.Errorf("store: decode style: %w", err)
		}
		out.Style, out.StyleTranslations, out.Follows = frozen.Style, frozen.Localizations, frozen.Follows
	}
	thanks, thanksByLang, err := thanksFromVersion(v)
	if err != nil {
		return out, err
	}
	out.Thanks, out.ThanksTranslations = thanks, thanksByLang
	langs, err := q.ListVersionLanguages(ctx, v.ID)
	if err != nil {
		return out, fmt.Errorf("store: list version languages: %w", err)
	}
	for _, lang := range langs {
		if out.QuestionTranslations == nil {
			out.QuestionTranslations = map[string]map[string]domain.LocalizedQuestion{}
		}
		if out.QuestionTranslations[lang] == nil {
			out.QuestionTranslations[lang] = map[string]domain.LocalizedQuestion{}
		}
	}
	return out, nil
}

// publishedVersion reads everything a version froze, as publishing it
// again needs it: its questions and their translations too.
func publishedVersion(ctx context.Context, q *db.Queries, v db.SurveyVersion) (domain.PublishedVersion, error) {
	out, err := publishedStyle(ctx, q, v)
	if err != nil {
		return out, err
	}
	if out.Questions, err = questionsForVersion(ctx, q, v.ID); err != nil {
		return out, err
	}
	rows, err := q.ListLocalizationsForVersion(ctx, v.ID)
	if err != nil {
		return out, fmt.Errorf("store: list localizations: %w", err)
	}
	for _, r := range rows {
		var options []string
		if len(r.Options) > 0 {
			if err := json.Unmarshal(r.Options, &options); err != nil {
				return out, fmt.Errorf("store: decode localized options: %w", err)
			}
		}
		if out.QuestionTranslations[r.Lang] == nil {
			if out.QuestionTranslations == nil {
				out.QuestionTranslations = map[string]map[string]domain.LocalizedQuestion{}
			}
			out.QuestionTranslations[r.Lang] = map[string]domain.LocalizedQuestion{}
		}
		out.QuestionTranslations[r.Lang][r.QuestionIdentityID.String()] = domain.LocalizedQuestion{Text: r.Text, Options: options}
	}
	return out, nil
}
