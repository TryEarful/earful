package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// answerSurvey fills and submits a respondent form, returning the body.
// Each call is a distinct respondent: fresh client, fresh render.
func answerSurvey(t *testing.T, app *apptest.App, surveyID string, answers map[int]string) string {
	t.Helper()
	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+surveyID)
	identities := extractAnswerFields(t, page)
	form := respondForm(t, page)
	for index, value := range answers {
		if index < len(identities) {
			form.Set("q_"+identities[index], value)
		}
	}
	_, body := submitAfterReading(t, app, respondent, surveyID, form)
	return body
}

// TestResults_AggregateAcrossVersionsByIdentity is ADR-0001's scenario in
// miniature: responses to version 1, the question reworded, responses to
// version 2, and one set of results that counts them together while
// showing that the wording changed.
func TestResults_AggregateAcrossVersionsByIdentity(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("results"))
	id := app.CreateSurvey(t, creator, "Cross-version", true)
	app.AddQuestion(t, creator, id, "single_choice", "How was it?", url.Values{
		"options": {"Good\nBad"},
	})
	app.AddQuestion(t, creator, id, "long_text", "Tell us more", nil)
	app.Publish(t, creator, id)

	for i := 0; i < 3; i++ {
		answerSurvey(t, app, id, map[int]string{0: "Good", 1: "Version one answer"})
	}

	// Reword the first question and publish again.
	identity := app.QuestionIdentities(t, creator, id)[0]
	app.PostForm(t, creator, "/surveys/"+id+"/questions/"+identity, url.Values{
		"type": {"single_choice"}, "text": {"Looking back, how was it?"}, "options": {"Good\nBad"},
	}).Body.Close()
	app.Publish(t, creator, id)

	answerSurvey(t, app, id, map[int]string{0: "Bad", 1: "Version two answer"})
	answerSurvey(t, app, id, map[int]string{0: "Good", 1: ""})

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")

	// One question, both wordings visible, answers counted together:
	// 4 Good/Bad answers across two versions, not two separate questions.
	if !bodyContains(page, "Looking back, how was it?") {
		t.Errorf("results do not show the current wording:\n%s", page)
	}
	if !bodyContains(page, "This question was reworded") || !bodyContains(page, "How was it?") {
		t.Errorf("results hid the rewording instead of labelling it:\n%s", page)
	}
	if !bodyContains(page, "5 responses") {
		t.Errorf("response count is wrong:\n%s", page)
	}
	if !bodyContains(page, "4 answers") {
		t.Errorf("choice answers were not aggregated across versions:\n%s", page)
	}
	// Text answers from both versions are listed, each labelled with the
	// version it was given under.
	for _, want := range []string{"Version one answer", "Version two answer", "v1", "v2"} {
		if !bodyContains(page, want) {
			t.Errorf("results missing %q:\n%s", want, page)
		}
	}
	// The respondent who left the text question blank is reported as a
	// skip, not counted as an empty answer.
	if !bodyContains(page, "1 respondent skipped this") {
		t.Errorf("skipped answers were not distinguished:\n%s", page)
	}
}

// TestResults_DistributionsPerType covers the countable types: choice,
// scale and NPS all read as distributions, with NPS carrying the score
// anyone using NPS is actually after.
func TestResults_DistributionsPerType(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("distributions"))
	id := app.CreateSurvey(t, creator, "Types", true)
	app.AddQuestion(t, creator, id, "rating_scale", "Rate it", url.Values{
		"scale_min": {"1"}, "scale_max": {"5"},
	})
	app.AddQuestion(t, creator, id, "nps", "Recommend us?", nil)
	app.AddQuestion(t, creator, id, "yes_no", "Would you return?", nil)
	app.Publish(t, creator, id)

	answerSurvey(t, app, id, map[int]string{0: "5", 1: "10", 2: "yes"})
	answerSurvey(t, app, id, map[int]string{0: "3", 1: "9", 2: "yes"})
	answerSurvey(t, app, id, map[int]string{0: "1", 1: "3", 2: "no"})

	page := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")

	if !bodyContains(page, "Average 3.0") {
		t.Errorf("rating average missing or wrong:\n%s", page)
	}
	// Two promoters (10, 9), one detractor (3): (2-1)/3 = +33.
	if !bodyContains(page, "NPS +33") {
		t.Errorf("NPS score missing or wrong:\n%s", page)
	}
	if !bodyContains(page, "2 promoters, 1 detractors, 0 passives") {
		t.Errorf("NPS breakdown missing:\n%s", page)
	}
	// Yes/No reads as a distribution with both options present, including
	// their share.
	if !bodyContains(page, "67%") {
		t.Errorf("yes/no distribution missing:\n%s", page)
	}
}

// TestResults_CSVIsSafeToOpen is M7-T2: one row per response, the version
// that row saw, and no cell a spreadsheet would execute.
func TestResults_CSVIsSafeToOpen(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("csv"))
	id := app.CreateSurvey(t, creator, "Export me", true)
	app.AddQuestion(t, creator, id, "long_text", "What happened?", nil)
	app.AddQuestion(t, creator, id, "single_choice", "Rating", url.Values{"options": {"Good\nBad"}})
	app.Publish(t, creator, id)

	answerSurvey(t, app, id, map[int]string{
		// The classic spreadsheet-injection payload, typed by a respondent.
		0: `=cmd|'/c calc'!A1`,
		1: "Good",
	})
	answerSurvey(t, app, id, map[int]string{0: "Ordinary answer, with a comma", 1: "Bad"})

	resp, err := creator.Get(app.Server.URL + "/surveys/" + id + "/results.csv")
	if err != nil {
		t.Fatalf("GET csv: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/csv") {
		t.Errorf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "export-me-responses.csv") {
		t.Errorf("Content-Disposition = %q", got)
	}
	body := apptest.ReadBody(t, resp)

	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) != 3 {
		t.Fatalf("csv has %d lines, want a header and two responses:\n%s", len(lines), body)
	}
	if !strings.HasPrefix(lines[0], "response_id,version,submitted_at,duration_secs,") {
		t.Errorf("header = %q", lines[0])
	}
	// Anonymous surveys have no participant column — there is nothing to
	// put in one (ADR-0003).
	if strings.Contains(lines[0], "participant_email") {
		t.Errorf("anonymous export leaked a participant column: %q", lines[0])
	}
	// The apostrophe is what stops Excel and Sheets evaluating the cell.
	if !strings.Contains(body, `,'=cmd|'/c calc'!A1,`) {
		t.Errorf("a formula cell was exported without the leading apostrophe:\n%s", body)
	}
	if !strings.Contains(body, `"Ordinary answer, with a comma"`) {
		t.Errorf("commas were not quoted:\n%s", body)
	}
}

// TestResults_InvitedSurveysCarryTheParticipant: story 49 read back —
// who answered what, in the results and in the export.
func TestResults_InvitedSurveysCarryTheParticipant(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("invitedresults"))
	id := app.CreateSurvey(t, creator, "Invited results", false)
	app.AddQuestion(t, creator, id, "long_text", "How did it go?", nil)
	app.Publish(t, creator, id)

	invitee := apptest.UniqueEmail("guest")
	app.PostForm(t, creator, "/surveys/"+id+"/participants", url.Values{"emails": {invitee}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/participants/send", nil).Body.Close()
	link := inviteLinkTo(t, app, invitee)

	participant := &http.Client{}
	page := mustGet(t, participant, link)
	form := respondForm(t, page)
	form.Set("q_"+extractAnswerFields(t, page)[0], "It went well")
	submitAfterReadingTo(t, app, participant, strings.TrimPrefix(link, app.Server.URL), form)

	results := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	if !bodyContains(results, invitee) {
		t.Errorf("invited results do not say who answered:\n%s", results)
	}

	resp, err := creator.Get(app.Server.URL + "/surveys/" + id + "/results.csv")
	if err != nil {
		t.Fatalf("GET csv: %v", err)
	}
	defer resp.Body.Close()
	csv := apptest.ReadBody(t, resp)
	if !strings.Contains(csv, "participant_email") || !strings.Contains(csv, invitee) {
		t.Errorf("invited export is missing the participant column:\n%s", csv)
	}
}

// TestResults_CrossWorkspaceDenied: results and exports are as
// workspace-scoped as everything else.
func TestResults_CrossWorkspaceDenied(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("resultsowner"))
	stranger := app.Login(t, apptest.UniqueEmail("resultsstranger"))
	id := app.CreateSurvey(t, owner, "Private results", true)
	app.AddQuestion(t, owner, id, "long_text", "Secret question", nil)
	app.Publish(t, owner, id)

	for _, path := range []string{"/surveys/" + id + "/results", "/surveys/" + id + "/results.csv"} {
		resp, err := stranger.Get(app.Server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, resp.StatusCode)
		}
		if bodyContains(body, "Secret question") {
			t.Errorf("%s leaked another workspace's survey", path)
		}
	}
}
