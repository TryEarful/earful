package http_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/apptest"
)

// The stats page (issue #2, ADR-0012): flow counters with a date, read
// back over a range the creator picks.

// TestStatsPage_RangeFiltersDatedCounts: three submissions on two days,
// and a range that covers only the second day sees only that day.
func TestStatsPage_RangeFiltersDatedCounts(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("statsrange"))
	id := app.CreateSurvey(t, creator, "Ranged", true)
	app.AddQuestion(t, creator, id, "short_text", "Only question", nil)
	app.Publish(t, creator, id)

	firstDay := app.Clock.Now().UTC().Format("2006-01-02")
	answerSurvey(t, app, id, map[int]string{0: "day one"})
	answerSurvey(t, app, id, map[int]string{0: "day one again"})
	app.Clock.Advance(3 * 24 * time.Hour)
	secondDay := app.Clock.Now().UTC().Format("2006-01-02")
	answerSurvey(t, app, id, map[int]string{0: "day four"})

	base := app.Server.URL + "/surveys/" + id + "/stats"

	all := mustGet(t, creator, base)
	if got := extractStat(t, all, "Submissions"); got != 3 {
		t.Errorf("all-time submissions = %d, want 3", got)
	}
	if got := extractStat(t, all, "Opened"); got != 3 {
		t.Errorf("all-time opened = %d, want 3", got)
	}
	if !bodyContains(all, "<td>"+firstDay+"</td>") || !bodyContains(all, "<td>"+secondDay+"</td>") {
		t.Errorf("trend table is missing a day with activity:\n%s", all)
	}

	narrow := mustGet(t, creator, base+"?from="+secondDay+"&to="+secondDay)
	if got := extractStat(t, narrow, "Submissions"); got != 1 {
		t.Errorf("one-day submissions = %d, want 1", got)
	}
	if bodyContains(narrow, "<td>"+firstDay+"</td>") {
		t.Errorf("a day outside the range appears in the trend table:\n%s", narrow)
	}
	// The CSV link carries the same range, so it downloads what is shown.
	if !bodyContains(narrow, "/stats.csv?from="+secondDay+"&to="+secondDay) {
		t.Errorf("CSV link does not carry the range:\n%s", narrow)
	}

	// Presets are ranges too: seven days back from today covers both days.
	week := mustGet(t, creator, base+"?range=7d")
	if got := extractStat(t, week, "Submissions"); got != 3 {
		t.Errorf("7-day submissions = %d, want 3", got)
	}

	// Garbage falls back to all-time rather than to an error page.
	garbage := mustGet(t, creator, base+"?from=yesterday&to=whenever")
	if got := extractStat(t, garbage, "Submissions"); got != 3 {
		t.Errorf("unparseable range did not fall back to all-time: submissions = %d", got)
	}
}

// TestStatsPage_LegacyTotalsCountOnlyAllTime: counters from before per-day
// tracking have no date. They are added to the all-time view, said so,
// and left out of any narrower range, said so too.
func TestStatsPage_LegacyTotalsCountOnlyAllTime(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("statslump"))
	id := app.CreateSurvey(t, creator, "Old survey", true)
	app.AddQuestion(t, creator, id, "short_text", "A question", nil)
	app.Publish(t, creator, id)
	answerSurvey(t, app, id, map[int]string{0: "dated, day one"})
	app.Clock.Advance(24 * time.Hour)
	answerSurvey(t, app, id, map[int]string{0: "dated, day two"})

	// The shape a survey answered before migration 00016 is left with.
	pool, err := pgxpool.New(context.Background(), app.DSN)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()
	for _, row := range []struct {
		metric, bucket string
		count          int
	}{{"start", "", 10}, {"completion", "", 4}, {"reached", "1", 4}} {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO survey_stats (survey_id, metric, bucket, count) VALUES ($1, $2, $3, $4)`,
			id, row.metric, row.bucket, row.count); err != nil {
			t.Fatalf("seed legacy counter: %v", err)
		}
	}

	base := app.Server.URL + "/surveys/" + id + "/stats"
	all := mustGet(t, creator, base)
	if got := extractStat(t, all, "Opened"); got != 12 {
		t.Errorf("all-time opened = %d, want 10 legacy + 2 dated", got)
	}
	if got := extractStat(t, all, "Submissions"); got != 6 {
		t.Errorf("all-time submissions = %d, want 4 legacy + 2 dated", got)
	}
	if !bodyContains(all, "Includes 10 opens and 4 submissions counted before per-day tracking") {
		t.Errorf("all-time view does not explain the undated counts:\n%s", all)
	}
	if !bodyContains(all, "4 earlier submissions were counted by question position") {
		t.Errorf("all-time view does not explain the undated stops:\n%s", all)
	}

	today := app.Clock.Now().UTC().Format("2006-01-02")
	narrow := mustGet(t, creator, base+"?from="+today+"&to="+today)
	if got := extractStat(t, narrow, "Opened"); got != 1 {
		t.Errorf("one-day opened = %d, want the dated 1 only", got)
	}
	if !bodyContains(narrow, "included in the all-time view only") {
		t.Errorf("narrow view does not say where the undated counts went:\n%s", narrow)
	}
}

// TestStatsPage_StopsFollowQuestionIdentity: "where answers stop" is keyed
// by Question Identity, so a question inserted ahead of it in a later
// version does not move the earlier counts onto the wrong row.
func TestStatsPage_StopsFollowQuestionIdentity(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("statsidentity"))
	id := app.CreateSurvey(t, creator, "Shifting", true)
	app.AddQuestion(t, creator, id, "short_text", "Original first", nil)
	app.AddQuestion(t, creator, id, "short_text", "Original second", nil)
	app.Publish(t, creator, id)

	// Two responses stop at the first question, one at the second.
	answerSurvey(t, app, id, map[int]string{0: "stop here"})
	answerSurvey(t, app, id, map[int]string{0: "stop here too"})
	answerSurvey(t, app, id, map[int]string{0: "on", 1: "to the end"})

	// Version 2 inserts a new question at the top and rewords the old
	// first one. By position, "stopped at 1" would now point at the
	// newcomer.
	original := app.QuestionIdentities(t, creator, id)
	app.AddQuestion(t, creator, id, "short_text", "Newcomer", nil)
	newcomer := app.QuestionIdentities(t, creator, id)[2]
	for i := 0; i < 2; i++ {
		app.PostForm(t, creator, "/surveys/"+id+"/questions/"+newcomer+"/move",
			url.Values{"direction": {"up"}}).Body.Close()
	}
	app.PostForm(t, creator, "/surveys/"+id+"/questions/"+original[0], url.Values{
		"type": {"short_text"}, "text": {"Reworded first"},
	}).Body.Close()
	app.Publish(t, creator, id)
	if order := app.QuestionIdentities(t, creator, id); order[0] != newcomer || order[1] != original[0] {
		t.Fatalf("version 2 order = %v, want newcomer first then %s", order, original[0])
	}

	// One more response to version 2 stops at the reworded question.
	answerSurvey(t, app, id, map[int]string{1: "stop at the reworded one"})

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/stats")
	if got := extractStopped(t, page, "Reworded first"); got != 3 {
		t.Errorf("stopped at the reworded question = %d, want 2 from v1 + 1 from v2", got)
	}
	if got := extractStopped(t, page, "Original second"); got != 1 {
		t.Errorf("stopped at the second question = %d, want 1", got)
	}
	if got := extractStopped(t, page, "Newcomer"); got != 0 {
		t.Errorf("the inserted question inherited %d stops that belong to another question", got)
	}
}

// TestStatsPage_MedianTimeToComplete: the median survives one abandoned
// tab, and the page says how many responses it is a median of.
func TestStatsPage_MedianTimeToComplete(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("statsmedian"))
	id := app.CreateSurvey(t, creator, "Timed", true)
	app.AddQuestion(t, creator, id, "short_text", "A question", nil)
	app.Publish(t, creator, id)

	// submitAfterReading adds five seconds of reading time to each, so
	// these land as 40, 50, 60 and 3600 seconds.
	for _, secs := range []int{35, 45, 55, 3595} {
		answerTimed(t, app, id, secs)
	}
	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/stats")
	if !bodyContains(page, "<dd>55s</dd>") {
		t.Errorf("median of 40/50/60/3600 should read 55s:\n%s", page)
	}
	if !bodyContains(page, "median of 4 timed responses") {
		t.Errorf("sample size missing:\n%s", page)
	}
}

// answerTimed submits one response whose client-stamped start was
// `secs` before the submit.
func answerTimed(t *testing.T, app *apptest.App, surveyID string, secs int) {
	t.Helper()
	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+surveyID)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "timed answer")
	started := app.Clock.Now().Add(-time.Duration(secs) * time.Second)
	form.Set("started_at", strconv.FormatInt(started.UnixMilli(), 10))
	submitAfterReading(t, app, respondent, surveyID, form)
}

// TestStatsPage_CSV: one block per day, one per question, and a question
// that looks like a formula is defused like any other exported cell.
func TestStatsPage_CSV(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("statscsv"))
	id := app.CreateSurvey(t, creator, "CSV stats", true)
	app.AddQuestion(t, creator, id, "short_text", "=HYPERLINK(\"https://evil\")", nil)
	app.Publish(t, creator, id)
	answerSurvey(t, app, id, map[int]string{0: "an answer"})
	today := app.Clock.Now().UTC().Format("2006-01-02")

	resp, err := creator.Get(app.Server.URL + "/surveys/" + id + "/stats.csv")
	if err != nil {
		t.Fatalf("GET stats.csv: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("content type = %q", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "csv-stats-stats-") {
		t.Errorf("filename does not name the survey and the range: %q", cd)
	}
	if !strings.HasPrefix(body, "day,opened,submissions\n") {
		t.Errorf("no day block header:\n%s", body)
	}
	if !strings.Contains(body, today+",1,1\n") {
		t.Errorf("today's row missing:\n%s", body)
	}
	if !strings.Contains(body, "\nquestion,type,stopped_here,share\n") {
		t.Errorf("no question block header:\n%s", body)
	}
	if !strings.Contains(body, `"'=HYPERLINK(""https://evil"")",short_text,1,100%`) {
		t.Errorf("formula-shaped question not defused:\n%s", body)
	}
}
