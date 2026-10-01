package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
)

// TestRespond_DateQuestion: a date question is the browser's own date
// control, and only a real day in yyyy-mm-dd is accepted. A refused
// answer is shown again with the reason, as every other refusal is.
func TestRespond_DateQuestion(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("date-respond"))
	id := app.CreateSurvey(t, creator, "Visits", true)
	app.AddQuestion(t, creator, id, "date", "When did you visit?", url.Values{"required": {"on"}})
	app.Publish(t, creator, id)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	identities := extractAnswerFields(t, page)
	if len(identities) != 1 {
		t.Fatalf("expected 1 answer field, got %d", len(identities))
	}
	field := "q_" + identities[0]
	if !strings.Contains(page, `type="date" name="`+field+`"`) {
		t.Errorf("a date question is not a native date control:\n%s", page)
	}

	form := respondForm(t, page)
	form.Set(field, "2026-02-30")
	resp, body := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("a day that does not exist: status = %d, want 422", resp.StatusCode)
	}
	if !bodyContains(body, "enter a date as year, month and day") {
		t.Errorf("missing the date message:\n%s", body)
	}
	if !strings.Contains(body, `value="2026-02-30"`) {
		t.Errorf("re-render lost what the respondent gave:\n%s", body)
	}
	if bodyContains(body, "Thank you") {
		t.Error("a day that does not exist was accepted")
	}

	form = respondForm(t, body)
	form.Set(field, "2026-04-18")
	resp, body = submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Thank you") {
		t.Fatalf("a valid date was refused: status %d\n%s", resp.StatusCode, body)
	}

	// Stored as given: the CSV carries the ISO date, the form an importer
	// and a spreadsheet both read.
	csv := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results.csv")
	if !strings.Contains(csv, "2026-04-18") {
		t.Errorf("the CSV does not carry the date answer:\n%s", csv)
	}
}

// TestResults_DateCountsPerDay: a date question reads as a count per
// day, earliest first, with each day in the reader's language.
func TestResults_DateCountsPerDay(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("date-results"))
	id := app.CreateSurvey(t, creator, "Visit days", true)
	app.AddQuestion(t, creator, id, "date", "When did you visit?", nil)
	app.Publish(t, creator, id)

	answerSurvey(t, app, id, map[int]string{0: "2026-04-18"})
	answerSurvey(t, app, id, map[int]string{0: "2026-04-18"})
	answerSurvey(t, app, id, map[int]string{0: "2026-04-03"})
	answerSurvey(t, app, id, map[int]string{0: ""})

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	if !bodyContains(page, "3 answers") {
		t.Errorf("date answers were not counted:\n%s", page)
	}
	if !bodyContains(page, "1 respondent skipped this") {
		t.Errorf("a blank date was not reported as a skip:\n%s", page)
	}
	early, late := strings.Index(page, "3 April 2026"), strings.Index(page, "18 April 2026")
	if early < 0 || late < 0 {
		t.Fatalf("days are not shown in the reader's language:\n%s", page)
	}
	if early > late {
		t.Error("days are not listed earliest first")
	}
	if !bodyContains(page, "67%") || !bodyContains(page, "33%") {
		t.Errorf("per day shares missing:\n%s", page)
	}
}

// TestGenerate_ProposesDateQuestions: the model is told the date type
// exists, and a date question it proposes reaches the draft and the
// respondent as one.
func TestGenerate_ProposesDateQuestions(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{GenerateScript: [][]string{{
		`{"type":"date","text":"When did you last visit?","options":["ignored"],"scale_min":1,"scale_max":5}` + "\n",
	}}}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("date-generate"))
	id := app.CreateSurvey(t, creator, "Generated visits", true)

	resp := app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{
		"prompt": {"when people visit the museum"},
	})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "When did you last visit?") {
		t.Fatalf("the generated date question is not in the draft:\n%s", body)
	}
	if len(fake.GenerateCalls) != 1 || !strings.Contains(fake.GenerateCalls[0].System, "date") {
		t.Errorf("the system prompt does not offer the date type")
	}

	app.Publish(t, creator, id)
	page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	if !strings.Contains(page, `type="date"`) {
		t.Errorf("the generated question is not a date question:\n%s", page)
	}
}
