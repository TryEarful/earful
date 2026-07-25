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

// InsightRun is one AI reading of a survey's answers (M10-T1). It is
// append-only: a run records what the responses said at a moment, and
// the label it always carries — model and timestamp — has to stay true.
type InsightRun struct {
	ID        uuid.UUID
	SurveyID  uuid.UUID
	Watermark *time.Time
	// ResponseCount is how many responses the run read. With Watermark it
	// forms the cache key: same newest response, same count, same answer.
	ResponseCount int
	Model         string
	Output        string
	CreatedAt     time.Time
}

// Fresh reports whether this run still reflects the current responses,
// so a re-run can serve it instead of spending money (story 54).
func (r InsightRun) Fresh(watermark *time.Time, count int) bool {
	if r.ResponseCount != count {
		return false
	}
	switch {
	case r.Watermark == nil && watermark == nil:
		return true
	case r.Watermark == nil || watermark == nil:
		return false
	default:
		return r.Watermark.Equal(*watermark)
	}
}

// CreateInsightRun stores a completed run.
func (s *Surveys) CreateInsightRun(ctx context.Context, run InsightRun, now time.Time) (InsightRun, error) {
	row, err := s.q.CreateInsightRun(ctx, db.CreateInsightRunParams{
		SurveyID:          run.SurveyID,
		ResponseWatermark: run.Watermark,
		ResponseCount:     int32(run.ResponseCount),
		Model:             run.Model,
		Output:            run.Output,
		CreatedAt:         now,
	})
	if err != nil {
		return InsightRun{}, fmt.Errorf("store: create insight run: %w", err)
	}
	return insightFromRow(row.ID, row.SurveyID, row.ResponseWatermark, row.ResponseCount,
		row.Model, row.Output, row.CreatedAt), nil
}

// LatestInsightRun returns the newest run for a survey, or ErrNotFound.
func (s *Surveys) LatestInsightRun(ctx context.Context, surveyID uuid.UUID) (InsightRun, error) {
	row, err := s.q.LatestInsightRun(ctx, surveyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return InsightRun{}, ErrNotFound
	}
	if err != nil {
		return InsightRun{}, fmt.Errorf("store: latest insight run: %w", err)
	}
	return insightFromRow(row.ID, row.SurveyID, row.ResponseWatermark, row.ResponseCount,
		row.Model, row.Output, row.CreatedAt), nil
}

func insightFromRow(id, surveyID uuid.UUID, watermark *time.Time, count int32,
	model, output string, createdAt time.Time) InsightRun {
	return InsightRun{
		ID: id, SurveyID: surveyID, Watermark: watermark, ResponseCount: int(count),
		Model: model, Output: output, CreatedAt: createdAt,
	}
}

// Watermark is the newest response and how many there are — what an
// insight run is cached against.
func (r Results) Watermark() (*time.Time, int) {
	var newest *time.Time
	for i := range r.Responses {
		at := r.Responses[i].SubmittedAt
		if newest == nil || at.After(*newest) {
			copy := at
			newest = &copy
		}
	}
	return newest, len(r.Responses)
}
