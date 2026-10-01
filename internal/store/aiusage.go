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

// AIUsage is one accounted AI call (M6-T2).
type AIUsage struct {
	WorkspaceID uuid.UUID
	SurveyID    *uuid.UUID
	Kind        string
	Tokens      int64
	EstCostEUR  float64
	// DurationSecs is seconds of audio for a transcription, zero for
	// everything else — voice is capped by duration, not by output size
	// (M5-T4).
	DurationSecs int
	Day          time.Time
}

// AddAIUsage records one call's consumption.
func (s *Surveys) AddAIUsage(ctx context.Context, usage AIUsage) error {
	var surveyID uuid.NullUUID
	if usage.SurveyID != nil {
		surveyID = uuid.NullUUID{UUID: *usage.SurveyID, Valid: true}
	}
	err := s.q.AddAIUsage(ctx, db.AddAIUsageParams{
		WorkspaceID:  uuid.NullUUID{UUID: usage.WorkspaceID, Valid: true},
		SurveyID:     surveyID,
		Kind:         usage.Kind,
		Tokens:       usage.Tokens,
		EstCost:      usage.EstCostEUR,
		DurationSecs: int32(usage.DurationSecs),
		Day:          usage.Day,
	})
	if err != nil {
		return fmt.Errorf("store: add ai usage: %w", err)
	}
	return nil
}

// AddAIUsageRecord is the flat-argument form ai.Meter consumes.
func (s *Surveys) AddAIUsageRecord(ctx context.Context, workspaceID uuid.UUID, surveyID *uuid.UUID, kind string, tokens int64, estCostEUR float64, durationSecs int, day time.Time) error {
	return s.AddAIUsage(ctx, AIUsage{
		WorkspaceID: workspaceID, SurveyID: surveyID, Kind: kind,
		Tokens: tokens, EstCostEUR: estCostEUR, DurationSecs: durationSecs, Day: day,
	})
}

// SurveyVoiceSecondsOnDay sums a survey's transcribed seconds for one day
// — the number the per-survey voice cap watches (M5-T4).
func (s *Surveys) SurveyVoiceSecondsOnDay(ctx context.Context, surveyID uuid.UUID, day time.Time) (int64, error) {
	seconds, err := s.q.SurveyVoiceSecondsOnDay(ctx, db.SurveyVoiceSecondsOnDayParams{
		SurveyID: uuid.NullUUID{UUID: surveyID, Valid: true},
		Day:      day,
	})
	if err != nil {
		return 0, fmt.Errorf("store: survey voice seconds: %w", err)
	}
	return seconds, nil
}

// WorkspaceTokensOnDay sums a workspace's tokens for one day.
func (s *Surveys) WorkspaceTokensOnDay(ctx context.Context, workspaceID uuid.UUID, day time.Time) (int64, error) {
	n, err := s.q.WorkspaceTokensOnDay(ctx, db.WorkspaceTokensOnDayParams{
		WorkspaceID: uuid.NullUUID{UUID: workspaceID, Valid: true},
		Day:         day,
	})
	if err != nil {
		return 0, fmt.Errorf("store: workspace tokens: %w", err)
	}
	return n, nil
}

// GlobalCostOnDay sums every workspace's estimated cost for one day —
// the number the € breaker watches.
func (s *Surveys) GlobalCostOnDay(ctx context.Context, day time.Time) (float64, error) {
	cost, err := s.q.GlobalCostOnDay(ctx, day)
	if err != nil {
		return 0, fmt.Errorf("store: global cost: %w", err)
	}
	return cost, nil
}

// WorkspaceAITier reads a workspace's AI tier (issue #3); a missing
// workspace is ErrNotFound.
func (s *Surveys) WorkspaceAITier(ctx context.Context, workspaceID uuid.UUID) (string, error) {
	tier, err := s.q.WorkspaceAITier(ctx, workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: workspace ai tier: %w", err)
	}
	return tier, nil
}

// SetWorkspaceAITier changes a live workspace's tier. The database
// rejects a tier it does not know; a deleted or missing workspace is
// ErrNotFound.
func (s *Surveys) SetWorkspaceAITier(ctx context.Context, workspaceID uuid.UUID, tier string) error {
	n, err := s.q.SetWorkspaceAITier(ctx, db.SetWorkspaceAITierParams{ID: workspaceID, AiTier: tier})
	if err != nil {
		return fmt.Errorf("store: set workspace ai tier: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// WorkspaceTier is one workspace as the tier control lists it.
type WorkspaceTier struct {
	ID   uuid.UUID
	Name string
	Tier string
}

// WorkspacesForAITier lists the live workspaces a live account belongs
// to. The address is matched as stored, which is lower case.
func (s *Surveys) WorkspacesForAITier(ctx context.Context, email string) ([]WorkspaceTier, error) {
	rows, err := s.q.WorkspacesForAITier(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("store: workspaces for ai tier: %w", err)
	}
	out := make([]WorkspaceTier, 0, len(rows))
	for _, r := range rows {
		out = append(out, WorkspaceTier{ID: r.ID, Name: r.Name, Tier: r.AiTier})
	}
	return out, nil
}
