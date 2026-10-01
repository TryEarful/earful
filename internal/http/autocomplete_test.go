package http_test

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

var (
	fieldTagRe     = regexp.MustCompile(`(?s)<(?:input|textarea)\b[^>]*>`)
	respondFormRe  = regexp.MustCompile(`(?s)<form\b[^>]*\bjs-respond-form\b.*?</form>`)
	mainRe         = regexp.MustCompile(`(?s)<main\b.*?</main>`)
	autocompleteRe = regexp.MustCompile(`\bautocomplete="off"`)
)

// fieldsAllowingAutofill returns the tags in markup a person fills in
// that do not carry autocomplete="off". Hidden fields are skipped because
// the HTML standard forbids "off" on them, a file picker because the
// attribute does not apply to it, and the honeypot (the one field with
// tabindex="-1") because it is excluded from the tab order on purpose.
func fieldsAllowingAutofill(markup string) (open []string, checked int) {
	for _, tag := range fieldTagRe.FindAllString(markup, -1) {
		if strings.Contains(tag, `type="hidden"`) || strings.Contains(tag, `type="file"`) ||
			strings.Contains(tag, `tabindex="-1"`) {
			continue
		}
		checked++
		if !autocompleteRe.MatchString(tag) {
			open = append(open, tag)
		}
	}
	return open, checked
}

// TestRespond_ControlsOptOutOfAutofill: an answer is never a login or an
// address, so no control on the respondent form invites the browser or a
// password manager to fill it, whatever the question type.
func TestRespond_ControlsOptOutOfAutofill(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("autofill"))
	id := app.CreateSurvey(t, creator, "Autofill", true)
	app.AddQuestion(t, creator, id, "long_text", "Tell us more", nil)
	app.AddQuestion(t, creator, id, "short_text", "Your team", nil)
	app.AddQuestion(t, creator, id, "single_choice", "Pick one", url.Values{"options": {"Alpha\nBeta"}})
	app.AddQuestion(t, creator, id, "dropdown", "Pick from the list", url.Values{"options": {"Red\nBlue"}})
	app.AddQuestion(t, creator, id, "multiple_choice", "Pick any", url.Values{"options": {"Tea\nCoffee"}})
	app.AddQuestion(t, creator, id, "rating_scale", "Rate it", url.Values{"scale_min": {"1"}, "scale_max": {"5"}})
	app.AddQuestion(t, creator, id, "nps", "Recommend us?", nil)
	app.AddQuestion(t, creator, id, "yes_no", "Again?", nil)
	app.Publish(t, creator, id)

	page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	form := respondFormRe.FindString(page)
	if form == "" {
		t.Fatalf("no respondent form on the page:\n%s", page)
	}
	if !strings.Contains(form, `name="website" tabindex="-1"`) {
		t.Fatalf("honeypot missing, so the exclusion below proves nothing:\n%s", form)
	}
	open, checked := fieldsAllowingAutofill(form)
	// 2 text fields, 2+2+2 choices, 5 scale points, 11 NPS points, yes and no.
	if checked < 24 {
		t.Fatalf("checked %d controls, want every answer control (24):\n%s", checked, form)
	}
	for _, tag := range open {
		t.Errorf("respondent control allows autofill: %s", tag)
	}
}

// TestSurvey_EditorFieldsOptOutOfAutofill: survey wording and its
// translations are never a login either, so the editor and the
// translation page keep the browser's remembered values out of them.
func TestSurvey_EditorFieldsOptOutOfAutofill(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("autofill-editor"))
	id := app.CreateSurvey(t, creator, "Autofill editor", false)
	app.AddQuestion(t, creator, id, "single_choice", "Pick one", url.Values{"options": {"Alpha\nBeta"}})
	app.AddQuestion(t, creator, id, "rating_scale", "Rate it", url.Values{"scale_min": {"1"}, "scale_max": {"5"}})
	app.PostForm(t, creator, "/surveys/"+id+"/localizations", url.Values{"lang": {"nl"}}).Body.Close()

	for _, path := range []string{"/surveys/new", "/surveys/" + id, "/surveys/" + id + "/localizations"} {
		page := getBody(t, creator, app.Server.URL+path)
		main := mainRe.FindString(page)
		open, checked := fieldsAllowingAutofill(main)
		if checked == 0 {
			t.Fatalf("%s: no fields found:\n%s", path, page)
		}
		for _, tag := range open {
			t.Errorf("%s: editor field allows autofill: %s", path, tag)
		}
	}
}
