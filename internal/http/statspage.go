package http

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TryEarful/earful/internal/audience"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/web/templates"
)

// The stats page (issue #2; ADR-0012). A survey's flow over time —
// opened, submitted, completion rate, time to complete, and the question
// where answers stop — for a date range the creator picks, plus the
// undated audience totals from ADR-0009.
//
// Everything on this page that has a date honours the range. The
// audience section has no date on purpose and says so. The page never
// suppresses the flow numbers: a count of opens or submissions reveals
// nothing about a person, and the responses behind them are already
// listed one by one on the results page.

const rangeInputLayout = "2006-01-02"

// statsRange is an inclusive span of UTC days.
type statsRange struct {
	From, To time.Time
	// AllTime is true when the span covers the survey's whole life, which
	// is the one range where the undated legacy totals belong in the sum.
	AllTime bool
	// Preset names the shortcut that produced the range ("7d", "30d",
	// "all") or is empty for a custom span.
	Preset string
}

// parseStatsRange reads ?range= or ?from=&to=, clamps to the survey's
// life, and falls back to all-time on anything it cannot read.
func parseStatsRange(r *http.Request, survey store.Survey, now time.Time) statsRange {
	today := store.DayOf(now)
	created := store.DayOf(survey.CreatedAt)
	if created.After(today) {
		created = today
	}
	rng := statsRange{From: created, To: today, AllTime: true, Preset: "all"}

	query := r.URL.Query()
	switch query.Get("range") {
	case "7d":
		rng.From, rng.To, rng.Preset = today.AddDate(0, 0, -6), today, "7d"
	case "30d":
		rng.From, rng.To, rng.Preset = today.AddDate(0, 0, -29), today, "30d"
	case "all", "":
		if query.Get("from") == "" && query.Get("to") == "" {
			return rng
		}
		from, errFrom := time.ParseInLocation(rangeInputLayout, query.Get("from"), time.UTC)
		to, errTo := time.ParseInLocation(rangeInputLayout, query.Get("to"), time.UTC)
		if errFrom != nil || errTo != nil {
			return rng
		}
		rng.From, rng.To, rng.Preset = from, to, ""
	default:
		return rng
	}

	if rng.From.After(rng.To) {
		rng.From, rng.To = rng.To, rng.From
	}
	if rng.From.Before(created) {
		rng.From = created
	}
	if rng.To.After(today) {
		rng.To = today
	}
	rng.AllTime = !rng.From.After(created) && !rng.To.Before(today)
	return rng
}

// statsReport is everything both the page and the CSV compute, before
// any formatting.
type statsReport struct {
	Range statsRange
	// Opened and Submissions are the range's sums, with the legacy totals
	// folded in only when the range is all-time.
	Opened, Submissions int
	// LumpOpened and LumpSubmissions are the undated totals from before
	// per-day counting; LumpStops is how many legacy "reached" counts
	// exist, which the by-question table cannot place.
	LumpOpened, LumpSubmissions, LumpStops int
	// Durations are the timed responses in range, ascending.
	Durations []int
	// Days is one entry per day in range, zero-filled.
	Days []statsDay
	// Questions is every identity in current order, with how many
	// submissions in range stopped at it.
	Questions []questionStop
	// InRangeSubmissions is the denominator for the stop shares: only
	// dated submissions are broken down by question.
	InRangeSubmissions int
	Audience           []store.SurveyStat
}

type statsDay struct {
	Day                 time.Time
	Opened, Submissions int
}

type questionStop struct {
	Question store.QuestionResults
	Stopped  int
}

// buildStatsReport gathers the counters, the durations and the questions
// for one survey and one range.
func (s *server) buildStatsReport(ctx context.Context, survey store.Survey, rng statsRange) (statsReport, error) {
	report := statsReport{Range: rng}

	totals, err := s.surveys.SurveyStats(ctx, survey.ID)
	if err != nil {
		return report, err
	}
	for _, stat := range totals {
		switch stat.Metric {
		case store.MetricStart:
			report.LumpOpened += stat.Count
		case store.MetricCompletion:
			report.LumpSubmissions += stat.Count
		case store.MetricReached:
			report.LumpStops += stat.Count
		case store.MetricBrowser, store.MetricDevice, store.MetricCountry:
			report.Audience = append(report.Audience, stat)
		}
	}

	daily, err := s.surveys.DailyStats(ctx, survey.ID, rng.From, rng.To)
	if err != nil {
		return report, err
	}
	// Index rather than pointer: the slice grows below, and a pointer
	// into it would go stale on the first reallocation.
	byDay := map[time.Time]int{}
	for day := rng.From; !day.After(rng.To); day = day.AddDate(0, 0, 1) {
		byDay[day] = len(report.Days)
		report.Days = append(report.Days, statsDay{Day: day})
	}
	stops := map[string]int{}
	for _, stat := range daily {
		index, inRange := byDay[store.DayOf(stat.Day)]
		switch stat.Metric {
		case store.MetricStart:
			report.Opened += stat.Count
			if inRange {
				report.Days[index].Opened += stat.Count
			}
		case store.MetricCompletion:
			report.Submissions += stat.Count
			if inRange {
				report.Days[index].Submissions += stat.Count
			}
		case store.MetricReached:
			stops[stat.Bucket] += stat.Count
		}
	}
	report.InRangeSubmissions = report.Submissions
	if rng.AllTime {
		report.Opened += report.LumpOpened
		report.Submissions += report.LumpSubmissions
	}

	questions, err := s.surveys.SurveyQuestions(ctx, survey.ID)
	if err != nil {
		return report, err
	}
	for _, question := range questions {
		report.Questions = append(report.Questions, questionStop{
			Question: question, Stopped: stops[question.IdentityID],
		})
	}

	report.Durations, err = s.surveys.ResponseDurations(ctx, survey.ID, rng.From, rng.To.AddDate(0, 0, 1))
	if err != nil {
		return report, err
	}
	return report, nil
}

func (s *server) surveyStatsPage(w http.ResponseWriter, r *http.Request) {
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	info, _ := authFrom(r.Context())
	now := s.clock.Now()
	rng := parseStatsRange(r, survey, now)
	report, err := s.buildStatsReport(r.Context(), survey, rng)
	if err != nil {
		s.internalError(w, r, "build survey stats", err)
		return
	}
	render(w, r, http.StatusOK, templates.SurveyStats(info.Email, info.CSRFToken,
		viewStatsPage(viewSurvey(survey, now), report, now)))
}

// viewStatsPage does every piece of formatting, so the template counts
// nothing and rounds nothing.
func viewStatsPage(survey templates.SurveyView, report statsReport, now time.Time) templates.SurveyStatsData {
	rng := report.Range
	data := templates.SurveyStatsData{
		Survey: survey,
		Range: templates.StatsRangeView{
			FromInput: rng.From.Format(rangeInputLayout),
			ToInput:   rng.To.Format(rangeInputLayout),
			MinInput:  rng.From.Format(rangeInputLayout),
			MaxInput:  store.DayOf(now).Format(rangeInputLayout),
			Label:     rangeLabel(rng),
			Preset:    rng.Preset,
			AllTime:   rng.AllTime,
		},
		CSVURL: "/surveys/" + survey.ID + "/stats.csv" + rangeQuery(rng),
	}
	if rng.AllTime {
		data.Range.MinInput = rng.From.Format(rangeInputLayout)
	}

	// Big picture.
	opened := report.Opened
	if opened < report.Submissions {
		// A survey answered before stats existed, or opens throttled away
		// by the anti-inflation limiter: never report more than 100%.
		opened = report.Submissions
	}
	data.Opened = strconv.Itoa(opened)
	data.Submissions = strconv.Itoa(report.Submissions)
	if opened > 0 {
		data.CompletionRate = fmt.Sprintf("%d%%", int(float64(report.Submissions)/float64(opened)*100+0.5))
	}
	if n := len(report.Durations); n > 0 {
		data.TimeToComplete = humanDuration(median(report.Durations))
		data.TimedNote = fmt.Sprintf("median of %s", timedLabel(n))
	}
	if lump := report.LumpOpened + report.LumpSubmissions; lump > 0 {
		if rng.AllTime {
			data.LumpNote = fmt.Sprintf("Includes %d opens and %d submissions counted before per-day tracking began; those have no date and do not appear on the chart.",
				report.LumpOpened, report.LumpSubmissions)
		} else {
			data.LumpNote = fmt.Sprintf("%d opens and %d submissions were counted before per-day tracking began; they have no date and are included in the all-time view only.",
				report.LumpOpened, report.LumpSubmissions)
		}
	}

	// Trends: the JSON carries every day, the table only the days with
	// something on them (a two-year survey has 730 days and most are 0).
	for _, day := range report.Days {
		point := templates.TrendPoint{
			Day: day.Day.Format(rangeInputLayout), Label: day.Day.Format("2 Jan"),
			Opened: day.Opened, Submissions: day.Submissions,
		}
		data.Trend = append(data.Trend, point)
		if day.Opened > 0 || day.Submissions > 0 {
			data.TrendRows = append(data.TrendRows, point)
		}
	}

	// Question by question.
	for _, stop := range report.Questions {
		row := templates.QuestionStopView{
			Text:      stop.Question.Text,
			TypeLabel: stop.Question.Type.Label(),
			Required:  stop.Question.Required,
			Stopped:   stop.Stopped,
		}
		if report.InRangeSubmissions > 0 {
			row.Percent = int(float64(stop.Stopped)/float64(report.InRangeSubmissions)*100 + 0.5)
		}
		row.Share = strconv.Itoa(row.Percent) + "%"
		data.Questions = append(data.Questions, row)
	}
	if rng.AllTime && report.LumpStops > 0 {
		data.StopsNote = fmt.Sprintf("%d earlier submissions were counted by question position before per-day tracking began and are not broken down here.",
			report.LumpStops)
	}

	// Audience: undated, suppressed below five, exactly as before.
	byMetric := map[string][]store.SurveyStat{}
	for _, stat := range report.Audience {
		byMetric[stat.Metric] = append(byMetric[stat.Metric], stat)
	}
	data.Browsers = suppressedBuckets(byMetric[store.MetricBrowser])
	data.Devices = suppressedBuckets(byMetric[store.MetricDevice])
	data.Countries = suppressedBuckets(byMetric[store.MetricCountry])
	data.HasAudience = len(data.Browsers)+len(data.Devices)+len(data.Countries) > 0
	data.SuppressionNote = fmt.Sprintf(
		"Groups with fewer than %d responses are hidden, so a small sample can't point at anyone.",
		audience.SuppressBelow)
	return data
}

func rangeLabel(rng statsRange) string {
	if rng.From.Equal(rng.To) {
		return rng.From.Format(dayLayout)
	}
	return rng.From.Format(dayLayout) + " – " + rng.To.Format(dayLayout)
}

// rangeQuery reproduces the range as a query string, so the CSV link
// downloads exactly what the page shows.
func rangeQuery(rng statsRange) string {
	if rng.AllTime {
		return ""
	}
	return "?from=" + rng.From.Format(rangeInputLayout) + "&to=" + rng.To.Format(rangeInputLayout)
}

// median of an ascending list; the mean of the two middles when even.
func median(sorted []int) int {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if !sort.IntsAreSorted(sorted) {
		sorted = append([]int(nil), sorted...)
		sort.Ints(sorted)
	}
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func timedLabel(n int) string {
	if n == 1 {
		return "1 timed response"
	}
	return strconv.Itoa(n) + " timed responses"
}

// surveyStatsCSV is the page as a spreadsheet: one row per day in range,
// then a blank line and one row per question. Two blocks with their own
// headers open fine in Excel and Sheets, and the range is in the file
// name so two downloads never look alike.
func (s *server) surveyStatsCSV(w http.ResponseWriter, r *http.Request) {
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	rng := parseStatsRange(r, survey, s.clock.Now())
	report, err := s.buildStatsReport(r.Context(), survey, rng)
	if err != nil {
		s.internalError(w, r, "build survey stats", err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+statsCSVFilename(survey.Title, rng)+`"`)
	if err := writeStatsCSV(w, report); err != nil {
		// Headers are already out; all that is left is to say so in the log.
		s.logger.Error("writing stats csv failed", "error", err)
	}
}

func statsCSVFilename(title string, rng statsRange) string {
	base := strings.TrimSuffix(csvFilename(title), "-responses.csv")
	return base + "-stats-" + rng.From.Format(rangeInputLayout) + "-" + rng.To.Format(rangeInputLayout) + ".csv"
}

func writeStatsCSV(w io.Writer, report statsReport) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"day", "opened", "submissions"}); err != nil {
		return err
	}
	for _, day := range report.Days {
		if err := cw.Write([]string{
			day.Day.Format(rangeInputLayout), strconv.Itoa(day.Opened), strconv.Itoa(day.Submissions),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	if err := cw.Write([]string{"question", "type", "stopped_here", "share"}); err != nil {
		return err
	}
	for _, stop := range report.Questions {
		share := "0%"
		if report.InRangeSubmissions > 0 {
			share = strconv.Itoa(int(float64(stop.Stopped)/float64(report.InRangeSubmissions)*100+0.5)) + "%"
		}
		if err := cw.Write([]string{
			csvSafe(stop.Question.Text), string(stop.Question.Type), strconv.Itoa(stop.Stopped), share,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
