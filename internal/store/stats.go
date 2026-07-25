package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/store/db"
)

// Survey stats (ADR-0009): counters about a survey, never about a
// respondent. The metric names are fixed by a CHECK constraint, so a
// typo — or an inventive new dimension — fails loudly instead of quietly
// widening what the product knows.
const (
	MetricStart      = "start"      // a respondent opened the survey
	MetricCompletion = "completion" // a response was submitted
	MetricReached    = "reached"    // bucket: the last question position answered
	MetricBrowser    = "browser"    // bucket: browser family
	MetricDevice     = "device"     // bucket: phone/tablet/desktop
	MetricCountry    = "country"    // bucket: ISO 3166-1 alpha-2
)

// SurveyStat is one counter.
type SurveyStat struct {
	Metric string
	Bucket string
	Count  int
}

// IncrementStat bumps one counter. Callers treat failures as
// unimportant: a lost statistic must never cost a respondent their
// answer.
func (s *Surveys) IncrementStat(ctx context.Context, surveyID uuid.UUID, metric, bucket string) error {
	err := s.q.IncrementSurveyStat(ctx, db.IncrementSurveyStatParams{
		SurveyID: surveyID, Metric: metric, Bucket: bucket,
	})
	if err != nil {
		return fmt.Errorf("store: increment %s stat: %w", metric, err)
	}
	return nil
}

// SurveyStats reads a survey's counters.
func (s *Surveys) SurveyStats(ctx context.Context, surveyID uuid.UUID) ([]SurveyStat, error) {
	rows, err := s.q.ListSurveyStats(ctx, surveyID)
	if err != nil {
		return nil, fmt.Errorf("store: list survey stats: %w", err)
	}
	out := make([]SurveyStat, 0, len(rows))
	for _, row := range rows {
		out = append(out, SurveyStat{Metric: row.Metric, Bucket: row.Bucket, Count: int(row.Count)})
	}
	return out, nil
}
