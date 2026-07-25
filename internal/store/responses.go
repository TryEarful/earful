package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store/db"
)

// PublicSurvey is what a respondent's share link resolves to. It carries
// only what a respondent may see: the title, who is running it, whether
// it is anonymous, and whether it is accepting answers. Notably absent is
// anything about the workspace beyond its display name.
type PublicSurvey struct {
	ID    uuid.UUID
	Title string
	// WorkspaceID never reaches the page. It is here because AI spent on
	// a respondent's behalf — transcribing a spoken answer — has to be
	// charged to the workspace that owns the survey (M5-T4).
	WorkspaceID   uuid.UUID
	WorkspaceName string
	IsAnonymous   bool
	CloseAt       *time.Time
	ClosedAt      *time.Time
	LatestVersion int
}

func (p PublicSurvey) State() domain.SurveyState {
	return domain.SurveyState{
		HasPublishedVersion: p.LatestVersion > 0,
		CloseAt:             p.CloseAt,
		ClosedAt:            p.ClosedAt,
	}
}

// ServedVersion is the exact version a respondent was shown, with its
// questions. A submission pins to this (ADR-0001).
type ServedVersion struct {
	ID        uuid.UUID
	Number    int
	Questions []domain.Question
}

// PublicSurvey resolves a share link. The link itself is the credential,
// so there is no workspace scoping here — but a soft-deleted survey, or
// one whose workspace is deleted, resolves to nothing.
func (s *Surveys) PublicSurvey(ctx context.Context, surveyID uuid.UUID) (PublicSurvey, error) {
	row, err := s.q.GetPublicSurvey(ctx, surveyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicSurvey{}, ErrNotFound
	}
	if err != nil {
		return PublicSurvey{}, fmt.Errorf("store: get public survey: %w", err)
	}
	return PublicSurvey{
		ID: row.ID, Title: row.Title,
		WorkspaceID: row.WorkspaceID, WorkspaceName: row.WorkspaceName,
		IsAnonymous: row.IsAnonymous, CloseAt: row.CloseAt, ClosedAt: row.ClosedAt,
		LatestVersion: int(row.LatestVersion),
	}, nil
}

// LatestServedVersion loads the version a new respondent should be shown.
func (s *Surveys) LatestServedVersion(ctx context.Context, surveyID uuid.UUID) (ServedVersion, error) {
	latest, err := s.q.GetLatestVersion(ctx, surveyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ServedVersion{}, ErrNotFound
	}
	if err != nil {
		return ServedVersion{}, fmt.Errorf("store: latest version: %w", err)
	}
	questions, err := s.QuestionsForVersion(ctx, latest.ID)
	if err != nil {
		return ServedVersion{}, err
	}
	return ServedVersion{ID: latest.ID, Number: int(latest.Number), Questions: questions}, nil
}

// ServedVersionByID reloads the exact version a respondent's form was
// rendered from, so a submission is validated and pinned against what
// they actually saw — even if a newer version was published while they
// were filling it in (SPEC.md story 32).
func (s *Surveys) ServedVersionByID(ctx context.Context, surveyID, versionID uuid.UUID) (ServedVersion, error) {
	row, err := s.q.GetVersion(ctx, db.GetVersionParams{ID: versionID, SurveyID: surveyID})
	if errors.Is(err, pgx.ErrNoRows) {
		// Either no such version, or it belongs to a different survey.
		return ServedVersion{}, ErrNotFound
	}
	if err != nil {
		return ServedVersion{}, fmt.Errorf("store: get version: %w", err)
	}
	questions, err := s.QuestionsForVersion(ctx, row.ID)
	if err != nil {
		return ServedVersion{}, err
	}
	return ServedVersion{ID: row.ID, Number: int(row.Number), Questions: questions}, nil
}

// ErrAlreadySubmitted is returned when an invited participant tries to
// answer twice.
var ErrAlreadySubmitted = errors.New("store: participant has already submitted")

// SubmitAnswers writes one response and its answers in a single
// transaction, pinned to the version served.
//
// participantID is nil for anonymous surveys and stays NULL forever
// (ADR-0003) — this function is the only place a response is created, so
// that is the whole of the anonymity guarantee on the write path.
func (s *Surveys) SubmitAnswers(
	ctx context.Context,
	surveyID uuid.UUID,
	version ServedVersion,
	participantID *uuid.UUID,
	answers map[string]domain.AnswerValue,
	durationSecs *int,
	now time.Time,
) (uuid.UUID, error) {
	questionIDs, err := s.questionIDsByIdentity(ctx, version.ID)
	if err != nil {
		return uuid.Nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: begin submit: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	qtx := s.q.WithTx(tx)

	var participant uuid.NullUUID
	if participantID != nil {
		participant = uuid.NullUUID{UUID: *participantID, Valid: true}
	}
	var duration *int32
	if durationSecs != nil {
		d := int32(*durationSecs)
		duration = &d
	}

	response, err := qtx.CreateResponse(ctx, db.CreateResponseParams{
		SurveyID:      surveyID,
		VersionID:     version.ID,
		ParticipantID: participant,
		DurationSecs:  duration,
		SubmittedAt:   now,
	})
	if err != nil {
		// The one-response-per-participant unique index is the real
		// enforcement; a racing double submit lands here.
		if isUniqueViolation(err) {
			return uuid.Nil, ErrAlreadySubmitted
		}
		return uuid.Nil, fmt.Errorf("store: create response: %w", err)
	}

	for _, q := range version.Questions {
		value, ok := answers[q.IdentityID]
		if !ok || value.IsEmpty() {
			// Unanswered optional questions simply have no answer row,
			// which keeps "skipped" and "answered blank" distinguishable.
			continue
		}
		encoded, err := value.Encode()
		if err != nil {
			return uuid.Nil, err
		}
		identityID, err := uuid.Parse(q.IdentityID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("store: bad question identity: %w", err)
		}
		if err := qtx.CreateAnswer(ctx, db.CreateAnswerParams{
			ResponseID:         response.ID,
			QuestionID:         questionIDs[q.IdentityID],
			QuestionIdentityID: identityID,
			Value:              encoded,
		}); err != nil {
			return uuid.Nil, fmt.Errorf("store: create answer: %w", err)
		}
	}

	if participantID != nil {
		if err := qtx.MarkParticipantSubmitted(ctx, db.MarkParticipantSubmittedParams{
			ID: *participantID, SubmittedAt: &now,
		}); err != nil {
			return uuid.Nil, fmt.Errorf("store: mark participant submitted: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("store: commit submit: %w", err)
	}
	return response.ID, nil
}

// questionIDsByIdentity maps each Question Identity in a version to that
// version's concrete question row.
func (s *Surveys) questionIDsByIdentity(ctx context.Context, versionID uuid.UUID) (map[string]uuid.UUID, error) {
	rows, err := s.q.ListQuestionsForVersion(ctx, versionID)
	if err != nil {
		return nil, fmt.Errorf("store: list questions: %w", err)
	}
	out := make(map[string]uuid.UUID, len(rows))
	for _, r := range rows {
		out[r.QuestionIdentityID.String()] = r.ID
	}
	return out, nil
}

// ResponseCount reports how many responses a survey has collected.
func (s *Surveys) ResponseCount(ctx context.Context, surveyID uuid.UUID) (int, error) {
	n, err := s.q.CountResponsesForSurvey(ctx, surveyID)
	if err != nil {
		return 0, fmt.Errorf("store: count responses: %w", err)
	}
	return int(n), nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

// SoftDeleteResponse hides a response from results, stats and every
// export, and starts its 30-day clock (M8-T1). Nothing is erased here:
// support can restore it until `earful purge` reaches it.
func (s *Surveys) SoftDeleteResponse(ctx context.Context, surveyID, responseID uuid.UUID, now time.Time) error {
	rows, err := s.q.SoftDeleteResponse(ctx, db.SoftDeleteResponseParams{
		ID: responseID, SurveyID: surveyID, DeletedAt: &now,
	})
	if err != nil {
		return fmt.Errorf("store: soft delete response: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
