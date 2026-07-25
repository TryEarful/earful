package http_test

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestStats_CountAndSuppress is M7-T4's product half: the numbers a
// creator gets, and the n < 5 rule that keeps a small sample from
// pointing at anybody (ADR-0009).
func TestStats_CountAndSuppress(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("stats"))
	id := app.CreateSurvey(t, creator, "Stats", true)
	app.AddQuestion(t, creator, id, "short_text", "First question", nil)
	app.AddQuestion(t, creator, id, "short_text", "Second question", nil)
	app.Publish(t, creator, id)

	// Six respondents finish; one answers only the first question.
	for i := 0; i < 5; i++ {
		answerSurvey(t, app, id, map[int]string{0: "one", 1: "two"})
	}
	answerSurvey(t, app, id, map[int]string{0: "only the first"})

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")

	if !bodyContains(page, "How this survey is going") {
		t.Fatalf("no stats panel:\n%s", page)
	}
	if !bodyContains(page, "Completion rate") {
		t.Errorf("completion rate missing:\n%s", page)
	}
	// Five responses stopped at question 2, one at question 1: the second
	// bucket is above the suppression threshold, the first is not — and
	// "where answers stop" is labelled as exactly that, not as drop-off.
	if !bodyContains(page, "Where answers stop") {
		t.Errorf("last-answered breakdown missing:\n%s", page)
	}
	if !bodyContains(page, "Question 2: Second question") {
		t.Errorf("last-answered bucket missing its question:\n%s", page)
	}

	// The audience section only appears once a bucket clears five: with
	// six identical requests the browser bucket does, so it shows, and
	// the page says why small groups are missing.
	if !bodyContains(page, "Groups with fewer than 5 responses are hidden") {
		t.Errorf("suppression is not explained:\n%s", page)
	}
}

// TestStats_SuppressBelowFive: four responses is not enough to show any
// audience bucket at all.
func TestStats_SuppressBelowFive(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("suppress"))
	id := app.CreateSurvey(t, creator, "Suppression", true)
	app.AddQuestion(t, creator, id, "short_text", "A question", nil)
	app.Publish(t, creator, id)

	for i := 0; i < 4; i++ {
		answerSurvey(t, app, id, map[int]string{0: "answer"})
	}

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	if bodyContains(page, "Audience") {
		t.Errorf("audience buckets shown with only four responses:\n%s", page)
	}
	// The counts that are not about a person are still shown.
	if !bodyContains(page, "4 responses") {
		t.Errorf("response count missing:\n%s", page)
	}
}

// TestStats_NoIdentifyingDataIsStored is the promise behind the numbers:
// a respondent's IP, user agent and country never reach the response
// path. What survives a submission is a counter on the survey.
func TestStats_NoIdentifyingDataIsStored(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("nostore"))
	id := app.CreateSurvey(t, creator, "No identity", true)
	app.AddQuestion(t, creator, id, "short_text", "A question", nil)
	app.Publish(t, creator, id)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "answered")
	req, err := http.NewRequest(http.MethodPost, app.Server.URL+"/s/"+id, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) "+
		"AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1")
	req.Header.Set("X-Forwarded-For", "81.169.145.68")
	app.Clock.Advance(5 * time.Second) // past the minimum fill time
	resp, err := respondent.Do(req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	defer resp.Body.Close()

	// Neither the user agent nor the address appears anywhere a creator
	// can read — not in results, not in the export.
	results := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	csv := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results.csv")
	for _, forbidden := range []string{"81.169.145.68", "AppleWebKit", "iPhone", "Mozilla"} {
		if strings.Contains(results, forbidden) {
			t.Errorf("results leaked %q", forbidden)
		}
		if strings.Contains(csv, forbidden) {
			t.Errorf("export leaked %q", forbidden)
		}
	}
}

// TestAggregatesCannotBeLinkedToResponses is ADR-0009's mechanical
// requirement: "an automated test must prove no application query can
// associate an aggregate with a response". Any query mentioning
// survey_stats alongside responses or answers fails the build — which is
// the only way to keep a counter a counter as the codebase grows.
func TestAggregatesCannotBeLinkedToResponses(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "db", "queries")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read queries dir: %v", err)
	}

	named := regexp.MustCompile(`(?m)^-- name: (\w+)`)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		// Split into individual queries so two unrelated queries in one
		// file do not read as a join.
		text := string(content)
		starts := named.FindAllStringIndex(text, -1)
		for i, start := range starts {
			end := len(text)
			if i+1 < len(starts) {
				end = starts[i+1][0]
			}
			query := strings.ToLower(text[start[0]:end])
			if !strings.Contains(query, "survey_stats") {
				continue
			}
			for _, table := range []string{"responses", "answers", "participants", "abuse_log"} {
				if strings.Contains(query, table) {
					t.Errorf("%s: a query touches survey_stats and %s together — "+
						"aggregates must have no join path to responses (ADR-0009):\n%s",
						entry.Name(), table, text[start[0]:end])
				}
			}
		}
	}
}

// TestAggregatesHaveNoForeignKeyToResponses checks the same rule where it
// is actually enforced: the schema. A counter with an FK to a response is
// not an aggregate, whatever the queries say today.
func TestAggregatesHaveNoForeignKeyToResponses(t *testing.T) {
	t.Parallel()
	migration, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "00011_survey_stats.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	text := strings.ToLower(string(migration))
	// The only reference survey_stats may hold is to the survey itself.
	for _, forbidden := range []string{"references responses", "references answers", "references participants"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("survey_stats gained %q — ADR-0009 forbids any join path to a response", forbidden)
		}
	}
	if !strings.Contains(text, "references surveys") {
		t.Error("survey_stats should reference the survey it counts")
	}
}

// TestStats_StartsAreRateLimited: a crawler must not be able to inflate
// the denominator of every completion rate.
func TestStats_StartsAreRateLimited(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("startflood"))
	id := app.CreateSurvey(t, creator, "Flood", true)
	app.AddQuestion(t, creator, id, "short_text", "A question", nil)
	app.Publish(t, creator, id)

	crawler := &http.Client{}
	for i := 0; i < 40; i++ {
		mustGet(t, crawler, app.Server.URL+"/s/"+id)
	}

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	// The limiter allows ten an hour from one network; the exact number
	// matters less than "far fewer than forty".
	if opened := extractOpened(t, page); opened > 15 {
		t.Errorf("40 page loads from one network counted as %d starts", opened)
	}
}

var openedRe = regexp.MustCompile(`(?s)<dt>Opened</dt>\s*<dd>(\d+)</dd>`)

func extractOpened(t *testing.T, page string) int {
	t.Helper()
	m := openedRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no opened count on the page:\n%s", page)
	}
	var n int
	for _, c := range m[1] {
		n = n*10 + int(c-'0')
	}
	return n
}
