package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TryEarful/earful/internal/store/db"
)

// Workspace suspension (ADR-0018, its safeguards). An operator suspends a
// workspace that is misusing the service; nothing is erased, and lifting
// the suspension puts everything back as it was. What a suspension stops
// is decided where each thing is done: the respondent's path refuses
// answers, the public picture address serves nothing, the AI meter
// refuses to spend, and the creator's routes that publish, reopen or
// send are held. This file only records the state.

// ErrAlreadySuspended refuses to suspend a workspace twice, which would
// overwrite who suspended it first and why.
var ErrAlreadySuspended = errors.New("store: workspace already suspended")

// ErrNotSuspended refuses to lift a suspension that is not there.
var ErrNotSuspended = errors.New("store: workspace not suspended")

// SuspendWorkspace suspends a live workspace. A missing or deleted one is
// ErrNotFound; one already suspended is ErrAlreadySuspended.
func (s *Surveys) SuspendWorkspace(ctx context.Context, workspaceID uuid.UUID, reason string, by uuid.UUID, at time.Time) error {
	n, err := s.q.SuspendWorkspace(ctx, db.SuspendWorkspaceParams{
		ID: workspaceID, Reason: reason, SuspendedAt: &at,
		SuspendedBy: uuid.NullUUID{UUID: by, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("store: suspend workspace: %w", err)
	}
	if n == 0 {
		return s.whyNotChanged(ctx, workspaceID, ErrAlreadySuspended)
	}
	return nil
}

// LiftWorkspaceSuspension ends a workspace's suspension. A missing or
// deleted workspace is ErrNotFound; one not suspended is ErrNotSuspended.
func (s *Surveys) LiftWorkspaceSuspension(ctx context.Context, workspaceID uuid.UUID) error {
	n, err := s.q.LiftWorkspaceSuspension(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("store: lift workspace suspension: %w", err)
	}
	if n == 0 {
		return s.whyNotChanged(ctx, workspaceID, ErrNotSuspended)
	}
	return nil
}

// whyNotChanged tells a workspace that is not there from one that is
// already in the state asked for, after an update that changed no row.
func (s *Surveys) whyNotChanged(ctx context.Context, workspaceID uuid.UUID, inState error) error {
	live, err := s.q.WorkspaceLive(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !live) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: read workspace: %w", err)
	}
	return inState
}

// WorkspaceSuspended reads whether a workspace is suspended. A missing
// workspace is not suspended: it has nothing to stop.
func (s *Surveys) WorkspaceSuspended(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	suspended, err := s.q.WorkspaceSuspended(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: workspace suspended: %w", err)
	}
	return suspended, nil
}

// SuspendedWorkspace is one suspended workspace as the operator's page
// lists it.
type SuspendedWorkspace struct {
	ID               uuid.UUID
	Name             string
	SuspendedAt      time.Time
	Reason           string
	MemberEmail      string
	SuspendedByEmail string
}

// SuspendedWorkspaces lists every suspended workspace, the longest
// suspended first.
func (s *Surveys) SuspendedWorkspaces(ctx context.Context) ([]SuspendedWorkspace, error) {
	rows, err := s.q.ListSuspendedWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: list suspended workspaces: %w", err)
	}
	out := make([]SuspendedWorkspace, 0, len(rows))
	for _, r := range rows {
		out = append(out, SuspendedWorkspace{
			ID: r.ID, Name: r.Name, SuspendedAt: r.SuspendedAt, Reason: r.Reason,
			MemberEmail: r.MemberEmail, SuspendedByEmail: r.SuspendedByEmail,
		})
	}
	return out, nil
}

// WorkspaceStanding is one workspace as the suspension control finds it
// through a member's address.
type WorkspaceStanding struct {
	ID        uuid.UUID
	Name      string
	Suspended bool
}

// WorkspacesForSuspension lists the live workspaces a live account
// belongs to. The address is matched as stored, which is lower case.
func (s *Surveys) WorkspacesForSuspension(ctx context.Context, email string) ([]WorkspaceStanding, error) {
	rows, err := s.q.WorkspacesForSuspension(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("store: workspaces for suspension: %w", err)
	}
	out := make([]WorkspaceStanding, 0, len(rows))
	for _, r := range rows {
		out = append(out, WorkspaceStanding{ID: r.ID, Name: r.Name, Suspended: r.SuspendedAt != nil})
	}
	return out, nil
}
