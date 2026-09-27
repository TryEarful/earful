package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/store/db"
)

// Survey stats (ADR-0009): counters about a survey, never about a
// respondent. The metric names are fixed by a CHECK constraint, so a
// typo — or an inventive new dimension — fails loudly instead of quietly
// widening what the product knows.
const (
	MetricStart      = "start"      // a respondent opened the survey (dated, ADR-0012)
	MetricCompletion = "completion" // a response was submitted (dated)
	MetricReached    = "reached"    // bucket: the last question answered — a Question Identity in the dated table, a position in the legacy totals
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

// Daily flow counters (ADR-0012). The same three rules as above, plus a
// day: the UTC date of the request, and nothing finer. Only the flow
// metrics are dated; an audience metric passed here fails the CHECK.

// DailyStat is one dated counter.
type DailyStat struct {
	Metric string
	Bucket string
	Day    time.Time
	Count  int
}

// IncrementDailyStat bumps one dated counter. Best effort, like
// IncrementStat: a lost statistic never costs a respondent their answer.
func (s *Surveys) IncrementDailyStat(ctx context.Context, surveyID uuid.UUID, metric, bucket string, day time.Time) error {
	err := s.q.IncrementSurveyStatDaily(ctx, db.IncrementSurveyStatDailyParams{
		SurveyID: surveyID, Metric: metric, Bucket: bucket, Day: DayOf(day),
	})
	if err != nil {
		return fmt.Errorf("store: increment daily %s stat: %w", metric, err)
	}
	return nil
}

// DailyStats reads a survey's dated counters between two days, inclusive.
func (s *Surveys) DailyStats(ctx context.Context, surveyID uuid.UUID, from, to time.Time) ([]DailyStat, error) {
	rows, err := s.q.ListSurveyStatsDaily(ctx, db.ListSurveyStatsDailyParams{
		SurveyID: surveyID, FromDay: DayOf(from), ToDay: DayOf(to),
	})
	if err != nil {
		return nil, fmt.Errorf("store: list daily survey stats: %w", err)
	}
	out := make([]DailyStat, 0, len(rows))
	for _, row := range rows {
		out = append(out, DailyStat{Metric: row.Metric, Bucket: row.Bucket, Day: row.Day, Count: int(row.Count)})
	}
	return out, nil
}

// AllDailyStats reads every dated counter a survey has, for the export.
func (s *Surveys) AllDailyStats(ctx context.Context, surveyID uuid.UUID) ([]DailyStat, error) {
	rows, err := s.q.ListAllSurveyStatsDaily(ctx, surveyID)
	if err != nil {
		return nil, fmt.Errorf("store: list all daily survey stats: %w", err)
	}
	out := make([]DailyStat, 0, len(rows))
	for _, row := range rows {
		out = append(out, DailyStat{Metric: row.Metric, Bucket: row.Bucket, Day: row.Day, Count: int(row.Count)})
	}
	return out, nil
}

// DayOf truncates an instant to its UTC calendar day, which is the only
// resolution the daily counters have.
func DayOf(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}
