package http

import (
	"context"
	"net/http"
	"net/netip"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/audience"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/geoip"
	"github.com/TryEarful/earful/internal/store"
)

// Survey stats and audience aggregates (M7-T4, ADR-0009; dated flow
// counters ADR-0012).
//
// What is recorded is a counter on a survey. What is not recorded, ever,
// is which response it came from — there is no column to put that in and
// no query that could ask. The IP resolves to a country and is dropped;
// the user agent resolves to a family and a device class and is dropped.
//
// Two tables, split by what a date would reveal. Opened, submitted and
// "where answers stopped" are counted per UTC day, because a creator
// wants to know how a survey went this week. Browser, device and
// country are counted as undated totals, because a per-day audience
// count next to a date picker would let two ranges be subtracted into
// one day's countries — and on a small anonymous survey one day is one
// person.
//
// Every write here is best effort. A statistic is worth having and never
// worth failing a respondent for, so errors are logged and swallowed.

// recordStart counts a respondent opening a survey. Rate-limited per
// network so a crawler cannot inflate the denominator of every
// completion rate on the instance.
func (s *server) recordStart(r *http.Request, surveyID uuid.UUID) {
	if !s.limitStats.Allow(s.clientIP(r) + "|" + surveyID.String()) {
		return
	}
	s.bumpDailyStat(r.Context(), surveyID, store.MetricStart, "")
}

// recordCompletion counts a submitted response and the three audience
// facts ADR-0009 blesses, plus the question where the respondent's
// answers stopped.
func (s *server) recordCompletion(r *http.Request, surveyID uuid.UUID,
	questions []domain.Question, submission domain.Submission) {
	ctx := r.Context()
	s.bumpDailyStat(ctx, surveyID, store.MetricCompletion, "")
	s.bumpStat(ctx, surveyID, store.MetricBrowser, audience.BrowserFamily(r.UserAgent()))
	s.bumpStat(ctx, surveyID, store.MetricDevice, audience.DeviceClass(r.UserAgent()))

	if addr, err := netip.ParseAddr(s.clientIP(r)); err == nil {
		if country, ok := geoip.Country(addr); ok {
			s.bumpStat(ctx, surveyID, store.MetricCountry, country)
		}
	}

	if identity := lastAnsweredIdentity(questions, submission); identity != "" {
		s.bumpDailyStat(ctx, surveyID, store.MetricReached, identity)
	}
}

func (s *server) bumpStat(ctx context.Context, surveyID uuid.UUID, metric, bucket string) {
	if err := s.surveys.IncrementStat(ctx, surveyID, metric, bucket); err != nil {
		s.logger.Error("recording survey stat failed", "metric", metric, "error", err)
	}
}

func (s *server) bumpDailyStat(ctx context.Context, surveyID uuid.UUID, metric, bucket string) {
	if err := s.surveys.IncrementDailyStat(ctx, surveyID, metric, bucket, s.clock.Now()); err != nil {
		s.logger.Error("recording daily survey stat failed", "metric", metric, "error", err)
	}
}

// lastAnsweredIdentity is the Question Identity of the last question a
// respondent actually answered.
//
// It is deliberately derived from submissions rather than from a
// progress beacon. Knowing where people abandon *before* submitting
// would need the respondent's browser to report each step, and adding a
// per-question call to a page that currently makes none is a poor trade
// on a product whose selling point is that respondent pages are quiet.
// The stats page says what this number means rather than calling it
// drop-off.
//
// Keyed by identity rather than position so that inserting a question
// in a later version does not shift every earlier count onto the wrong
// row (ADR-0001's reason for identities, applied to counters).
func lastAnsweredIdentity(questions []domain.Question, submission domain.Submission) string {
	last := ""
	for _, question := range questions {
		if value, ok := submission.Answers[question.IdentityID]; ok && !value.IsEmpty() {
			last = question.IdentityID
		}
	}
	return last
}
