package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store/db"
)

var (
	// ErrStarterExists is returned for a workspace that already holds a
	// live Starter Survey. A deleted one does not count.
	ErrStarterExists = errors.New("store: the workspace already holds a starter survey")
	// ErrOwnerUnknown is returned when no live account has the address,
	// or the account has no workspace.
	ErrOwnerUnknown = errors.New("store: no workspace is owned by that address")
)

// SeedStarterSurvey writes a workspace's Starter Survey (story 86,
// ADR-0015) through q: the survey, its draft, the revision that records
// the draft, and version 1. It is written inside the caller's
// transaction, so that a workspace and its first survey exist together
// or not at all.
//
// The survey is anonymous, since only an anonymous survey has an address
// anyone can open, and that cannot be changed afterwards (ADR-0003). It
// is saved and published in the owner's name: the Audit Log shows an
// actor it cannot name as a deleted account.
func SeedStarterSurvey(ctx context.Context, q *db.Queries, workspaceID, userID uuid.UUID, title string, draft domain.Draft, now time.Time) (db.Survey, error) {
	if err := domain.ValidateTitle(title); err != nil {
		return db.Survey{}, err
	}
	if err := draft.ValidateForPublish(); err != nil {
		return db.Survey{}, err
	}
	if err := draft.ReadyToPublish(); err != nil {
		return db.Survey{}, err
	}

	live, err := q.CountLiveStarterSurveys(ctx, workspaceID)
	if err != nil {
		return db.Survey{}, fmt.Errorf("store: count starter surveys: %w", err)
	}
	if live > 0 {
		return db.Survey{}, ErrStarterExists
	}

	survey, err := createSurvey(ctx, q, workspaceID, userID, title, true, nil, OriginStarter)
	if err != nil {
		return db.Survey{}, err
	}
	if err := saveDraft(ctx, q, survey.ID, userID, draft, now); err != nil {
		return db.Survey{}, err
	}
	if _, err := publishDraft(ctx, q, survey.ID, userID, draft, now); err != nil {
		return db.Survey{}, err
	}
	return survey, nil
}

// AddStarterSurvey gives the workspace owned by ownerEmail a Starter
// Survey. It is for a workspace made before every workspace was given
// one, and for an owner who deleted theirs and wants it back.
func (s *Surveys) AddStarterSurvey(ctx context.Context, ownerEmail, title string, draft domain.Draft, now time.Time) (Survey, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Survey{}, fmt.Errorf("store: begin add starter survey: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	qtx := s.q.WithTx(tx)
	user, err := qtx.GetUserByEmail(ctx, strings.ToLower(strings.TrimSpace(ownerEmail)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Survey{}, ErrOwnerUnknown
	}
	if err != nil {
		return Survey{}, fmt.Errorf("store: get owner: %w", err)
	}
	workspace, err := qtx.GetWorkspaceForUser(ctx, user.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Survey{}, ErrOwnerUnknown
	}
	if err != nil {
		return Survey{}, fmt.Errorf("store: get owner's workspace: %w", err)
	}

	row, err := SeedStarterSurvey(ctx, qtx, workspace.ID, user.ID, title, draft, now)
	if err != nil {
		return Survey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Survey{}, fmt.Errorf("store: commit add starter survey: %w", err)
	}
	return surveyFromRow(row, 1, len(draft.Questions)), nil
}
