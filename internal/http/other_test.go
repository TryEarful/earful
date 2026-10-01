package http_test

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/export"
)

// otherSurvey publishes a survey with a single choice, a dropdown and a
// multiple choice question, each offering Other, and returns its id.
func otherSurvey(t *testing.T, app *apptest.App, creator *http.Client, title string, required bool) string {
	t.Helper()
	id := app.CreateSurvey(t, creator, title, true)
	extra := url.Values{"options": {"Email\nPhone"}, "allow_other": {"on"}}
	if required {
		extra.Set("required", "on")
	}
	app.AddQuestion(t, creator, id, "single_choice", "How did you hear of us?", extra)
	app.AddQuestion(t, creator, id, "dropdown", "Which region?", url.Values{
		"options": {"North\nSouth"}, "allow_other": {"on"},
	})
	app.AddQuestion(t, creator, id, "multiple_choice", "How do you keep in touch?", url.Values{
		"options": {"Email\nPhone"}, "allow_other": {"on"},
	})
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("publish refused:\n%s", body)
	}
	return id
}

// TestRespond_OtherOnEveryChoiceType: Other is offered after the
// options on each of the three choice types, what is written beside it
// is stored, read back on the thanks page, counted as a bar of its own
// in the results, listed under it, and written into the CSV.
func TestRespond_OtherOnEveryChoiceType(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("other-types"))
	id := otherSurvey(t, app, creator, "Other everywhere", false)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	fields := extractAnswerFields(t, page)
	if len(fields) != 3 {
		t.Fatalf("expected 3 answer fields, got %d", len(fields))
	}
	for _, field := range fields {
		for _, want := range []string{
			`name="q_` + field + `" value="` + domain.OtherChoice + `"`,
			`name="other_` + field + `"`,
			`aria-labelledby="qt-` + field + ` qo-` + field + `"`,
		} {
			if !strings.Contains(page, want) {
				t.Errorf("the Other control lacks %s:\n%s", want, page)
			}
		}
	}
	if strings.Count(page, `class="js-other-text"`) != 3 {
		t.Errorf("want one box beside each Other:\n%s", page)
	}

	form := respondForm(t, page)
	form.Set("q_"+fields[0], domain.OtherChoice)
	form.Set("other_"+fields[0], "  A <b>friend</b> told me  ")
	form.Set("q_"+fields[1], domain.OtherChoice)
	form.Set("other_"+fields[1], "Abroad")
	form["q_"+fields[2]] = []string{"Email", domain.OtherChoice}
	form.Set("other_"+fields[2], "Carrier pigeon")
	resp, thanks := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(thanks, "Thank you") {
		t.Fatalf("an Other answer was refused: status %d\n%s", resp.StatusCode, thanks)
	}
	for _, want := range []string{"Other: A <b>friend</b> told me", "Other: Abroad", "Email, Other: Carrier pigeon"} {
		if !bodyContains(thanks, want) {
			t.Errorf("the thanks page does not read back %q:\n%s", want, thanks)
		}
	}
	if strings.Contains(thanks, "<b>friend</b>") {
		t.Error("what was written beside Other reached the page unescaped")
	}

	// A second respondent picks an option, so Other is one bar among
	// the options rather than the only one.
	page = mustGet(t, respondent, app.Server.URL+"/s/"+id)
	form = respondForm(t, page)
	form.Set("q_"+fields[0], "Email")
	submitAfterReading(t, app, respondent, id, form)

	results := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	for _, want := range []string{"Written for Other", "A <b>friend</b> told me", "Abroad", "Carrier pigeon"} {
		if !bodyContains(results, want) {
			t.Errorf("the results lack %q:\n%s", want, results)
		}
	}
	if strings.Contains(results, "<b>friend</b>") {
		t.Error("what was written beside Other reached the results unescaped")
	}
	if strings.Contains(results, domain.OtherChoice) {
		t.Errorf("the marker is shown to the creator:\n%s", results)
	}
	if strings.Count(results, `<span class="bar-label">Other</span>`) != 3 {
		t.Errorf("want an Other bar on each of the three questions:\n%s", results)
	}
	if !bodyContains(results, "1 (50%)") {
		t.Errorf("Other is not counted beside the option picked:\n%s", results)
	}

	resp, err := creator.Get(app.Server.URL + "/surveys/" + id + "/results.csv")
	if err != nil {
		t.Fatalf("GET csv: %v", err)
	}
	records, err := csv.NewReader(strings.NewReader(apptest.ReadBody(t, resp))).ReadAll()
	resp.Body.Close()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("want a header and two rows, got %q", records)
	}
	for question, want := range map[string]string{
		"How did you hear of us?":   "Other: A <b>friend</b> told me",
		"Which region?":             "Other: Abroad",
		"How do you keep in touch?": "Email, Other: Carrier pigeon",
	} {
		column := slices.Index(records[0], question)
		if column < 0 || records[1][column] != want {
			t.Errorf("CSV column %q = %q, want %q", question, records[1], want)
		}
	}
}

// TestRespond_OtherNeedsWords: Other picked with nothing written is
// refused with a message saying where to write, on a required question
// and an optional one alike; words written with nothing picked are taken
// as Other, which is what a form without a script sends.
func TestRespond_OtherNeedsWords(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("other-empty"))
	id := otherSurvey(t, app, creator, "Other left empty", true)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	fields := extractAnswerFields(t, page)

	form := respondForm(t, page)
	form.Set("q_"+fields[0], domain.OtherChoice)
	form.Set("other_"+fields[0], "   ")
	form["q_"+fields[2]] = []string{domain.OtherChoice}
	resp, body := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if !bodyContains(body, "rite your answer in the box for Other") {
		t.Errorf("the message does not say where to write:\n%s", body)
	}
	if !strings.Contains(body, `value="`+domain.OtherChoice+`" class="js-other-choice" checked`) {
		t.Errorf("the re-render lost the Other choice:\n%s", body)
	}

	form = respondForm(t, page)
	form.Set("other_"+fields[0], "From a podcast")
	resp, body = submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Other: From a podcast") {
		t.Fatalf("words in the box with nothing picked were not taken as Other: status %d\n%s", resp.StatusCode, body)
	}
}

// TestRespond_OtherOnlyWhereOffered: a form that posts Other to a
// question that does not offer it is refused, and a box beside it is
// never read.
func TestRespond_OtherOnlyWhereOffered(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("other-absent"))
	id := app.CreateSurvey(t, creator, "No Other here", true)
	app.AddQuestion(t, creator, id, "single_choice", "Tea or coffee?", url.Values{"options": {"Tea\nCoffee"}})
	app.AddQuestion(t, creator, id, "multiple_choice", "Which biscuits?", url.Values{"options": {"Shortbread\nGinger"}})
	app.Publish(t, creator, id)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if strings.Contains(page, "js-other-choice") || strings.Contains(page, `name="other_`) {
		t.Fatalf("Other is offered on a question that does not offer it:\n%s", page)
	}
	fields := extractAnswerFields(t, page)

	for name, set := range map[string]func(url.Values){
		"single":   func(f url.Values) { f.Set("q_"+fields[0], domain.OtherChoice); f.Set("other_"+fields[0], "Water") },
		"multiple": func(f url.Values) { f["q_"+fields[1]] = []string{domain.OtherChoice}; f.Set("other_"+fields[1], "Oat") },
	} {
		form := respondForm(t, page)
		set(form)
		resp, body := submitAfterReading(t, app, respondent, id, form)
		if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "hoose one of the options offered") {
			t.Errorf("%s: Other was accepted where it is not offered: status %d\n%s", name, resp.StatusCode, body)
		}
	}

	// The box alone, beside a real option, is ignored rather than stored.
	form := respondForm(t, page)
	form.Set("q_"+fields[0], "Tea")
	form.Set("other_"+fields[0], "Water")
	resp, body := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK || bodyContains(body, "Water") {
		t.Errorf("a box the question does not have was read: status %d\n%s", resp.StatusCode, body)
	}
}

// TestRespond_OtherInATranslatedSurvey: Other is worded by the
// interface in the respondent's language, and the answer is stored with
// the marker and the words as written, beside choices mapped back to the
// creator's options.
func TestRespond_OtherInATranslatedSurvey(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: translatorFake()})
	creator := app.Login(t, apptest.UniqueEmail("other-lang"))
	id := app.CreateSurvey(t, creator, "Other in Spanish", true)
	app.AddQuestion(t, creator, id, "multiple_choice", "How do you travel?", url.Values{
		"options": {"Bus\nTrain"}, "allow_other": {"on"},
	})
	identities := app.QuestionIdentities(t, creator, id)
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"es"}}).Body.Close()
	app.PostForm(t, creator, "/surveys/"+id+"/localizations/es", url.Values{
		"t_" + identities[0]: {"¿Cómo viaja?"},
		"o_" + identities[0]: {"Autobús\nTren"},
	}).Body.Close()
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 1") {
		t.Fatalf("publish refused:\n%s", body)
	}

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id+"?lang=es")
	if !bodyContains(page, "¿Cómo viaja?") || !bodyContains(page, ">Otro</span>") {
		t.Fatalf("Other is not offered in the respondent's language:\n%s", page)
	}
	field := extractAnswerFields(t, page)[0]
	form := respondForm(t, page)
	form["q_"+field] = []string{"Tren", domain.OtherChoice}
	form.Set("other_"+field, "En bicicleta")
	_, thanks := submitAfterReadingTo(t, app, respondent, "/s/"+id+"?lang=es", form)
	if !bodyContains(thanks, "Tren, Otro: En bicicleta") {
		t.Fatalf("the answer is not read back in Spanish:\n%s", thanks)
	}

	results := mustGet(t, creator, app.Server.URL+"/surveys/"+id+"/results")
	if !bodyContains(results, "En bicicleta") || !bodyContains(results, `<span class="bar-label">Train</span>`) {
		t.Errorf("the answer was not stored against the creator's options:\n%s", results)
	}
}

// TestEditor_AllowOther: the editor offers Other on the choice types
// only, keeps it on the question, and publishing freezes it: turning it
// on alone is a change to publish.
func TestEditor_AllowOther(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("other-editor"))
	id := app.CreateSurvey(t, creator, "Editing Other", true)
	if page := app.SurveyPage(t, creator, id); !strings.Contains(page, `name="allow_other"`) ||
		!bodyContains(page, "Offer Other, with a box to write in") {
		t.Fatalf("the add form does not offer Other:\n%s", page)
	}

	// A ticked box on a type without options means nothing.
	app.AddQuestion(t, creator, id, "short_text", "Your name?", url.Values{"allow_other": {"on"}})
	app.AddQuestion(t, creator, id, "single_choice", "Tea or coffee?", url.Values{"options": {"Tea\nCoffee"}})
	app.Publish(t, creator, id)
	if page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id); strings.Contains(page, "js-other-choice") {
		t.Fatalf("Other is offered though no question asked for it:\n%s", page)
	}

	identity := app.QuestionIdentities(t, creator, id)[1]
	app.PostForm(t, creator, "/surveys/"+id+"/questions/"+identity, url.Values{
		"type": {"single_choice"}, "text": {"Tea or coffee?"}, "options": {"Tea\nCoffee"}, "allow_other": {"on"},
	}).Body.Close()
	editor := app.SurveyPage(t, creator, id)
	if !strings.Contains(editor, `name="allow_other" class="js-allow-other" checked`) {
		t.Errorf("the question does not show Other as offered:\n%s", editor)
	}
	if !bodyContains(editor, "Publish version 2") {
		t.Fatalf("offering Other alone is not a change to publish:\n%s", editor)
	}
	if body := app.Publish(t, creator, id); !bodyContains(body, "Published version 2") {
		t.Fatalf("publish refused:\n%s", body)
	}
	if page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id); !strings.Contains(page, "js-other-choice") {
		t.Errorf("the published version does not offer Other:\n%s", page)
	}

	// An option spelled like the marker would read back as Other.
	resp := app.PostForm(t, creator, "/surveys/"+id+"/questions/"+identity, url.Values{
		"type": {"single_choice"}, "text": {"Tea or coffee?"}, "options": {"Tea\n" + domain.OtherChoice}, "allow_other": {"on"},
	})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "hoose a different wording for that option") {
		t.Errorf("an option spelled like the marker was accepted:\n%s", body)
	}
}

// TestWorkspaceExport_CarriesOther: the export says which questions
// offered Other and what was written beside it.
func TestWorkspaceExport_CarriesOther(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("other-export"))
	id := otherSurvey(t, app, creator, "Exported Other", false)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	field := extractAnswerFields(t, page)[0]
	form := respondForm(t, page)
	form.Set("q_"+field, domain.OtherChoice)
	form.Set("other_"+field, "A billboard")
	submitAfterReading(t, app, respondent, id, form)

	link := waitForExport(t, app, creator)
	resp, err := creator.Get(app.Server.URL + link)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var archive export.Archive
	if err := json.Unmarshal(openArchive(t, raw)["workspace.json"], &archive); err != nil {
		t.Fatal(err)
	}
	if archive.FormatVersion != 6 {
		t.Errorf("format_version = %d, want 6", archive.FormatVersion)
	}
	for _, survey := range archive.Surveys {
		if survey.ID != id {
			continue
		}
		for _, q := range survey.Versions[0].Questions {
			if !q.AllowOther {
				t.Errorf("question %q lost allow_other", q.Text)
			}
		}
		answer := survey.Responses[0].Answers[field]
		if answer.Choice != domain.OtherChoice || answer.Other != "A billboard" {
			t.Errorf("answer = %+v", answer)
		}
		return
	}
	t.Fatal("the survey is not in the export")
}
