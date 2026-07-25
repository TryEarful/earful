package http

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/TryEarful/earful/internal/store/db"
	"github.com/TryEarful/earful/web/templates"
)

// Founder metrics (M9-T7). The numbers needed to steer the product,
// read from our own database by a super admin.
//
// What makes this ADR-0006-compatible is what it is *not*: nothing is
// added to a respondent page, no third-party analytics exists to add,
// and the completion rate comes from the unlinked survey counters rather
// than from anything joined to a response. The KPI definitions are in
// docs/metrics.md, because a number without a definition is an argument
// waiting to happen.

const metricsWindow = 30 * 24 * time.Hour

func (s *server) adminMetricsPage(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	ctx := r.Context()
	queries := db.New(s.pool)
	since := s.clock.Now().Add(-metricsWindow)

	totals, err := queries.MetricTotals(ctx)
	if err != nil {
		s.internalError(w, r, "read metric totals", err)
		return
	}
	signups, err := queries.MetricSignupsByDay(ctx, since)
	if err != nil {
		s.internalError(w, r, "read signups", err)
		return
	}
	responses, err := queries.MetricResponsesByDay(ctx, since)
	if err != nil {
		s.internalError(w, r, "read responses", err)
		return
	}
	cost, err := queries.MetricAICostByDay(ctx, since)
	if err != nil {
		s.internalError(w, r, "read ai cost", err)
		return
	}
	rates, err := queries.MetricCompletionRates(ctx)
	if err != nil {
		s.internalError(w, r, "read completion rates", err)
		return
	}

	data := templates.MetricsData{
		WindowDays: int(metricsWindow.Hours() / 24),
		Totals: []templates.MetricTotal{
			{Label: "Accounts", Value: strconv.FormatInt(totals.Users, 10)},
			{Label: "Workspaces", Value: strconv.FormatInt(totals.Workspaces, 10)},
			{Label: "Surveys", Value: strconv.FormatInt(totals.Surveys, 10)},
			{Label: "Published surveys", Value: strconv.FormatInt(totals.PublishedSurveys, 10)},
			{Label: "Responses", Value: strconv.FormatInt(totals.Responses, 10)},
			{Label: "Participants invited", Value: strconv.FormatInt(totals.Participants, 10)},
		},
	}
	if rates.Starts > 0 {
		data.Totals = append(data.Totals, templates.MetricTotal{
			Label: "Completion rate",
			Value: fmt.Sprintf("%d%%", int(float64(rates.Completions)/float64(rates.Starts)*100+0.5)),
		})
	}

	for _, row := range signups {
		data.Signups = append(data.Signups, templates.MetricPoint{
			Day: row.Day.Format(dayLayout), Value: strconv.FormatInt(row.Count, 10),
		})
	}
	for _, row := range responses {
		data.Responses = append(data.Responses, templates.MetricPoint{
			Day: row.Day.Format(dayLayout), Value: strconv.FormatInt(row.Count, 10),
		})
	}
	var totalCost float64
	for _, row := range cost {
		totalCost += row.Cost
		data.AICost = append(data.AICost, templates.MetricPoint{
			Day:   row.Day.Format(dayLayout),
			Value: fmt.Sprintf("€%.2f (%d tokens)", row.Cost, row.Tokens),
		})
	}
	data.AICostTotal = fmt.Sprintf("€%.2f", totalCost)
	data.BudgetNote = fmt.Sprintf("Daily breaker: €%.2f. Estimates, not invoices — see docs/metrics.md.",
		s.cfg.AIDailyBudgetEUR)

	render(w, r, http.StatusOK, templates.AdminMetrics(info.Email, info.CSRFToken, data))
}
