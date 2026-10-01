package http_test

import (
	"bytes"
	"encoding/csv"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
)

// TestRespond_NumberQuestion: a number question is the browser's own
// number field, held to the question's limits, and only a whole number
// within them is accepted. A refused answer is shown again with the
// limits, as every other refusal is.
func TestRespond_NumberQuestion(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("number-respond"))
	id := app.CreateSurvey(t, creator, "Party size", true)
	app.AddQuestion(t, creator, id, "number", "How many people did you come with?",
		url.Values{"number_min": {"1"}, "number_max": {"12"}, "required": {"on"}})
	app.Publish(t, creator, id)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	identities := extractAnswerFields(t, page)
	if len(identities) != 1 {
		t.Fatalf("expected 1 answer field, got %d", len(identities))
	}
	field := "q_" + identities[0]
	for _, want := range []string{
		`type="number" inputmode="numeric" name="` + field + `"`,
		`min="1" max="12" step="1"`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the number field lacks %s:\n%s", want, page)
		}
	}
	if !bodyContains(page, "A whole number from 1 to 12.") {
		t.Errorf("the limits are not said in words:\n%s", page)
	}

	for _, typed := range []string{"13", "0", "2.5", "a dozen"} {
		form := respondForm(t, page)
		form.Set(field, typed)
		resp, body := submitAfterReading(t, app, respondent, id, form)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%q: status = %d, want 422", typed, resp.StatusCode)
		}
		if !bodyContains(body, "enter a whole number from 1 to 12") {
			t.Errorf("%q: missing the number message:\n%s", typed, body)
		}
		if !bodyContains(body, `value="`+typed+`"`) {
			t.Errorf("%q: the re-render lost what the respondent typed:\n%s", typed, body)
		}
		if bodyContains(body, "Thank you") {
			t.Errorf("%q was accepted", typed)
		}
	}

	form := respondForm(t, page)
	form.Set(field, " 4 ")
	resp, body := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Thank you") {
		t.Fatalf("a whole number within the limits was refused: status %d\n%s", resp.StatusCode, body)
	}

	// The answer is a number in both downloads: plain digits in the CSV,
	// a numeric cell in the workbook.
	resp, err := creator.Get(app.Server.URL + "/surveys/" + id + "/results.csv")
	if err != nil {
		t.Fatalf("GET csv: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(apptest.ReadBody(t, resp))).ReadAll()
	resp.Body.Close()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	column := slices.Index(records[0], "How many people did you come with?")
	if column < 0 || len(records) != 2 || records[1][column] != "4" {
		t.Fatalf("the CSV does not carry the number 4: %q", records)
	}

	resp, err = creator.Get(app.Server.URL + "/surveys/" + id + "/results.xlsx")
	if err != nil {
		t.Fatalf("GET xlsx: %v", err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read xlsx: %v", err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("the workbook does not open: %v", err)
	}
	defer book.Close()
	sheet := book.GetSheetName(0)
	cell, err := excelize.CoordinatesToCellName(column+1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if kind, err := book.GetCellType(sheet, cell); err != nil ||
		(kind != excelize.CellTypeNumber && kind != excelize.CellTypeUnset) {
		t.Errorf("the number answer (%s) is not a numeric cell: %v %v", cell, kind, err)
	}
}

// TestRespond_NumberKeepsItsLimitsAcrossVersions: the limits are frozen
// at publish, so changing only them is a real change, and the live
// survey serves the new limits.
func TestRespond_NumberKeepsItsLimitsAcrossVersions(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("number-rebound"))
	id := app.CreateSurvey(t, creator, "Party size again", true)
	app.AddQuestion(t, creator, id, "number", "How many came?",
		url.Values{"number_min": {"-5"}, "number_max": {"5"}})
	app.Publish(t, creator, id)

	page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	if !strings.Contains(page, `min="-5" max="5"`) {
		t.Errorf("the published survey lost its limits:\n%s", page)
	}

	identity := app.QuestionIdentities(t, creator, id)[0]
	resp := app.PostForm(t, creator, "/surveys/"+id+"/questions/"+identity, url.Values{
		"type": {"number"}, "text": {"How many came?"},
		"number_min": {"0"}, "number_max": {"250"},
	})
	resp.Body.Close()
	body := app.Publish(t, creator, id)
	if !bodyContains(body, "Published version 2") {
		t.Errorf("new limits were treated as no change:\n%s", body)
	}

	page = mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	if !strings.Contains(page, `min="0" max="250"`) {
		t.Errorf("the live survey still serves the old limits:\n%s", page)
	}
	if !bodyContains(app.SurveyPage(t, creator, id), `name="number_max" min="-1000000" max="1000000" step="1" value="250"`) {
		t.Error("the editor does not show the question's limits")
	}
}

// TestResults_NumberSummary: a number question reads as its average,
// median, lowest and highest answer, not as a bar per value.
func TestResults_NumberSummary(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("number-results"))
	id := app.CreateSurvey(t, creator, "Party sizes", true)
	app.AddQuestion(t, creator, id, "number", "How many came?",
		url.Values{"number_min": {"1"}, "number_max": {"1000000"}})
	app.Publish(t, creator, id)

	empty := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	if bodyContains(empty, "Average") {
		t.Errorf("a summary of no answers was shown:\n%s", empty)
	}

	for _, given := range []string{"1", "3", "4", "1000000", ""} {
		answerSurvey(t, app, id, map[int]string{0: given})
	}

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	if !bodyContains(page, "4 answers") {
		t.Errorf("number answers were not counted:\n%s", page)
	}
	if !bodyContains(page, "1 respondent skipped this") {
		t.Errorf("a blank number was not reported as a skip:\n%s", page)
	}
	if !bodyContains(page, "Average 250002.0 · median 3.5 · lowest 1 · highest 1,000,000") {
		t.Errorf("the summary is missing or wrong:\n%s", page)
	}
	if strings.Contains(page, `class="distribution"`) {
		t.Error("a number question was drawn as a bar per value")
	}

	spanish := &http.Client{Jar: creator.Jar}
	req, err := http.NewRequest(http.MethodGet, app.Server.URL+"/surveys/"+id+"/results", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Language", "es")
	resp, err := spanish.Do(req)
	if err != nil {
		t.Fatalf("GET results in Spanish: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "mediana 3,5") {
		t.Errorf("the summary is not in the reader's language:\n%s", body)
	}
}

// TestGenerate_ProposesNumberQuestions: the model is told the number type
// and its limits, and a number question it proposes reaches the draft
// and the respondent with them.
func TestGenerate_ProposesNumberQuestions(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{GenerateScript: [][]string{{
		`{"type":"number","text":"How many people came with you?","options":["ignored"],"scale_min":1,"scale_max":12}` + "\n",
		`{"type":"number","text":"How many without limits?"}` + "\n",
	}}}
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("number-generate"))
	id := app.CreateSurvey(t, creator, "Generated party sizes", true)

	resp := app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{
		"prompt": {"how big the groups at the museum are"},
	})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "How many people came with you?") {
		t.Fatalf("the generated number question is not in the draft:\n%s", body)
	}
	if bodyContains(body, "How many without limits?") {
		t.Error("a number question without limits reached the draft")
	}
	if len(fake.GenerateCalls) != 1 || !strings.Contains(fake.GenerateCalls[0].System, "number asks for a whole number") {
		t.Errorf("the system prompt does not offer the number type")
	}

	app.Publish(t, creator, id)
	page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	if !strings.Contains(page, `type="number"`) || !strings.Contains(page, `min="1" max="12"`) {
		t.Errorf("the generated question is not a number question with its limits:\n%s", page)
	}
}
