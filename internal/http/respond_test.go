package http_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// publishedSurvey creates a published survey with the given questions and
// returns its id, driving the real creator endpoints.
func publishedSurvey(t *testing.T, app *apptest.App, client *http.Client, title string, anonymous bool, questions ...[3]string) string {
	t.Helper()
	id := app.CreateSurvey(t, client, title, anonymous)
	for _, q := range questions {
		extra := url.Values{}
		if q[2] != "" {
			extra.Set("options", q[2])
		}
		app.AddQuestion(t, client, id, q[0], q[1], extra)
	}
	app.Publish(t, client, id)
	return id
}

// TestRespond_AnonymousSubmission covers SPEC.md stories 28 and 41: a
// share link leads to an answerable survey, and submitting works.
func TestRespond_AnonymousSubmission(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("respond-anon"))
	id := publishedSurvey(t, app, creator, "Coffee habits", true,
		[3]string{"long_text", "How do you take your coffee?", ""},
		[3]string{"single_choice", "How many cups a day?", "One\nTwo\nThree or more"},
	)

	// A respondent has no account and no session.
	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if !bodyContains(page, "How do you take your coffee?") {
		t.Fatalf("share link did not render the survey:\n%s", page)
	}
	if !bodyContains(page, "It's anonymous") {
		t.Errorf("anonymous survey should disclose its anonymity:\n%s", page)
	}

	identities := extractAnswerFields(t, page)
	if len(identities) != 2 {
		t.Fatalf("expected 2 answer fields, got %d", len(identities))
	}

	form := respondForm(t, page)
	form.Set("q_"+identities[0], "Black, no sugar")
	form.Set("q_"+identities[1], "Two")
	resp, body := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("submit status = %d, want 200:\n%s", resp.StatusCode, body)
	}
	if !bodyContains(body, "Thank you") {
		t.Errorf("submission did not confirm:\n%s", body)
	}
}

// TestRespond_DropdownAnswersLikeAChoiceList: a dropdown renders as a
// lettered radio list rather than a <select>, because a browser draws
// its own option popup and there is nowhere in it to put a key hint
// (story 80). What must not change is the wire: same field name, same
// values, so handlers, validation, results and exports cannot tell the
// difference.
func TestRespond_DropdownAnswersLikeAChoiceList(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("respond-dropdown"))
	id := publishedSurvey(t, app, creator, "Where from", true,
		[3]string{"dropdown", "Where did you hear about us?", "A friend\nSearch\nAn ad"},
	)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if strings.Contains(page, "<select name=\"q_") {
		t.Errorf("dropdown still renders a <select>, which cannot carry a key hint:\n%s", page)
	}
	if !bodyContains(page, `type="radio"`) {
		t.Errorf("dropdown did not render selectable options:\n%s", page)
	}
	// The hint is decoration for the eye only; the accessible name must
	// stay the option text alone.
	if !bodyContains(page, `class="key-hint" data-key="A" aria-hidden="true"`) {
		t.Errorf("options carry no aria-hidden key hint:\n%s", page)
	}

	identities := extractAnswerFields(t, page)
	form := respondForm(t, page)
	form.Set("q_"+identities[0], "An ad")
	resp, body := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Thank you") {
		t.Fatalf("dropdown answer did not submit (status %d):\n%s", resp.StatusCode, body)
	}
}

// TestRespond_WorksWithoutJavaScript is story 29's core claim, and the
// reason the server renders the whole form: no test here executes any
// JavaScript, so everything these tests do is what a JS-less browser does.
func TestRespond_WorksWithoutJavaScript(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("nojs"))
	id := publishedSurvey(t, app, creator, "No JS needed", true,
		[3]string{"short_text", "First question", ""},
		[3]string{"short_text", "Second question", ""},
		[3]string{"short_text", "Third question", ""},
	)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)

	// Every question is present in the markup — not fetched on demand.
	for _, want := range []string{"First question", "Second question", "Third question"} {
		if !bodyContains(page, want) {
			t.Errorf("question %q missing from the server-rendered form:\n%s", want, page)
		}
	}
	// And a plain submit of the whole form is accepted. Note what is NOT
	// here: no ALTCHA solution, no JS-stamped fields — the no-JS path
	// clears the anti-abuse gauntlet through the signed render timestamp
	// and the tight rate bucket alone.
	identities := extractAnswerFields(t, page)
	form := respondForm(t, page)
	for i, identity := range identities {
		form.Set("q_"+identity, "answer "+string(rune('A'+i)))
	}
	_, body := submitAfterReading(t, app, respondent, id, form)
	if !bodyContains(body, "Thank you") {
		t.Errorf("JS-less submission was not accepted:\n%s", body)
	}
}

// TestRespond_RequiredAnswerValidation shows problems inline and keeps
// what the respondent already typed.
func TestRespond_RequiredAnswerValidation(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("required"))
	id := app.CreateSurvey(t, creator, "Required fields", true)
	app.AddQuestion(t, creator, id, "short_text", "Your name", url.Values{"required": {"on"}})
	app.AddQuestion(t, creator, id, "long_text", "Anything else?", nil)
	app.Publish(t, creator, id)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	identities := extractAnswerFields(t, page)

	form := respondForm(t, page)
	form.Set("q_"+identities[0], "")
	form.Set("q_"+identities[1], "Some optional thoughts I typed")
	resp, body := submitAfterReading(t, app, respondent, id, form)

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", resp.StatusCode)
	}
	if !bodyContains(body, "this question needs an answer") {
		t.Errorf("missing required-answer message:\n%s", body)
	}
	if !bodyContains(body, "Some optional thoughts I typed") {
		t.Errorf("re-render lost what the respondent already typed:\n%s", body)
	}
	if bodyContains(body, "Thank you") {
		t.Error("an incomplete submission was accepted")
	}
}

// TestRespond_PublishedRatingScaleKeepsItsBounds pins the defect fixed by
// migration 00009: publish used to drop ScaleMin/ScaleMax, so a live
// rating question rendered as a single radio labelled "0" and accepted
// only 0. Preview reads the draft and looked fine, which is exactly why
// this assertion has to run against the *published* share link.
func TestRespond_PublishedRatingScaleKeepsItsBounds(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("scale"))
	id := app.CreateSurvey(t, creator, "Scale bounds", true)
	app.AddQuestion(t, creator, id, "rating_scale", "Rate the onboarding",
		url.Values{"scale_min": {"1"}, "scale_max": {"7"}})
	app.Publish(t, creator, id)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	identity := extractAnswerFields(t, page)[0]

	for point := 1; point <= 7; point++ {
		want := `name="q_` + identity + `" value="` + strconv.Itoa(point) + `"`
		if !strings.Contains(page, want) {
			t.Errorf("scale point %d missing from the published survey:\n%s", point, page)
		}
	}
	if strings.Contains(page, `name="q_`+identity+`" value="0"`) {
		t.Error("a 1..7 scale offered 0")
	}

	form := respondForm(t, page)
	form.Set("q_"+identity, "7")
	resp, body := submitAfterReading(t, app, respondent, id, form)
	if resp.StatusCode != http.StatusOK || !bodyContains(body, "Thank you") {
		t.Errorf("top of the scale was refused (status %d):\n%s", resp.StatusCode, body)
	}
}

// TestSurvey_RescalingIsPublishable: changing only a rating question's
// bounds is a real change, not "nothing to publish" — the same defect
// seen from the creator's side.
func TestSurvey_RescalingIsPublishable(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("rescale"))
	id := app.CreateSurvey(t, creator, "Rescale", true)
	app.AddQuestion(t, creator, id, "rating_scale", "How was it?",
		url.Values{"scale_min": {"1"}, "scale_max": {"5"}})
	app.Publish(t, creator, id)

	identity := app.QuestionIdentities(t, creator, id)[0]
	resp := app.PostForm(t, creator, "/surveys/"+id+"/questions/"+identity, url.Values{
		"type": {"rating_scale"}, "text": {"How was it?"},
		"scale_min": {"1"}, "scale_max": {"10"},
	})
	resp.Body.Close()

	body := app.Publish(t, creator, id)
	if !bodyContains(body, "Published version 2") {
		t.Errorf("rescaling was treated as no change:\n%s", body)
	}

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	if !strings.Contains(page, `value="10"`) {
		t.Errorf("live survey still serves the old scale:\n%s", page)
	}
}

// TestRespond_RejectsValuesOutsideTheQuestion: a crafted POST cannot smuggle
// an option that was never offered, or a scale value off the scale.
func TestRespond_RejectsValuesOutsideTheQuestion(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("crafted"))
	id := publishedSurvey(t, app, creator, "Crafted values", true,
		[3]string{"single_choice", "Pick one", "Alpha\nBeta"},
		[3]string{"nps", "Recommend us?", ""},
	)

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	identities := extractAnswerFields(t, page)

	cases := []struct {
		name    string
		answers url.Values
		wantMsg string
	}{
		{
			name:    "option never offered",
			answers: url.Values{"q_" + identities[0]: {"Gamma"}},
			wantMsg: "choose one of the options offered",
		},
		{
			name:    "scale value out of range",
			answers: url.Values{"q_" + identities[1]: {"11"}},
			wantMsg: "choose a value on the scale",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := respondForm(t, page)
			for k, v := range tc.answers {
				form[k] = v
			}
			_, body := submitAfterReading(t, app, respondent, id, form)
			if !bodyContains(body, tc.wantMsg) {
				t.Errorf("expected %q, got:\n%s", tc.wantMsg, body)
			}
		})
	}
}

// TestRespond_PinsToTheVersionServed is SPEC.md story 32: publishing a new
// version while someone is mid-fill must not reattribute their answers.
func TestRespond_PinsToTheVersionServed(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("pinning"))
	id := publishedSurvey(t, app, creator, "Version pinning", true,
		[3]string{"short_text", "Original question", ""})

	// A respondent loads the form (version 1)...
	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	v1 := extractVersionID(t, page)
	identities := extractAnswerFields(t, page)

	// ...and while they type, the creator publishes version 2.
	app.AddQuestion(t, creator, id, "short_text", "Brand new question", nil)
	app.Publish(t, creator, id)

	// Their submission still succeeds, against version 1's questions.
	form := respondForm(t, page)
	form.Set("q_"+identities[0], "Answered before the change")
	_, body := submitAfterReading(t, app, respondent, id, form)
	if !bodyContains(body, "Thank you") {
		t.Fatalf("mid-fill submission was rejected after a republish:\n%s", body)
	}

	// A fresh respondent gets version 2, which has the new question.
	fresh := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	if !bodyContains(fresh, "Brand new question") {
		t.Errorf("new respondents should be served the latest version:\n%s", fresh)
	}
	if extractVersionID(t, fresh) == v1 {
		t.Error("new respondents are still being served version 1")
	}
}

// TestRespond_VersionFromAnotherSurveyRejected: the pinned version must
// belong to the survey being answered.
func TestRespond_VersionFromAnotherSurveyRejected(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("cross-version"))
	idA := publishedSurvey(t, app, creator, "Survey A", true, [3]string{"short_text", "A question", ""})
	idB := publishedSurvey(t, app, creator, "Survey B", true, [3]string{"short_text", "B question", ""})

	respondent := &http.Client{}
	pageA := mustGet(t, respondent, app.Server.URL+"/s/"+idA)
	pageB := mustGet(t, respondent, app.Server.URL+"/s/"+idB)

	form := respondForm(t, pageA)
	form.Set("version_id", extractVersionID(t, pageB))
	form.Set("q_"+extractAnswerFields(t, pageA)[0], "smuggled")
	resp, _ := submitAfterReading(t, app, respondent, idA, form)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a version from another survey", resp.StatusCode)
	}
}

// TestRespond_ClosedSurveyRefuses completes M3-T5's respondent-facing
// half: a closed survey says so plainly (story 31).
func TestRespond_ClosedSurveyRefuses(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("closed-respond"))
	id := publishedSurvey(t, app, creator, "Closing time", true,
		[3]string{"short_text", "Quick question", ""})

	respondent := &http.Client{}
	page := mustGet(t, respondent, app.Server.URL+"/s/"+id)
	identities := extractAnswerFields(t, page)

	resp := app.PostForm(t, creator, "/surveys/"+id+"/close", nil)
	resp.Body.Close()

	// The page now explains itself...
	closedResp, err := respondent.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatalf("GET closed survey: %v", err)
	}
	body := apptest.ReadBody(t, closedResp)
	closedResp.Body.Close()
	if closedResp.StatusCode != http.StatusGone {
		t.Errorf("status = %d, want 410", closedResp.StatusCode)
	}
	if !bodyContains(body, "This survey is closed") {
		t.Errorf("closed survey should say so:\n%s", body)
	}

	// ...and a form held open from before it closed cannot submit.
	form := respondForm(t, page)
	form.Set("q_"+identities[0], "too late")
	_, lateBody := submitAfterReading(t, app, respondent, id, form)
	if bodyContains(lateBody, "Thank you") {
		t.Error("a closed survey accepted a response")
	}
}

// TestRespond_UnpublishedSurveyIsNotAnswerable: a draft has nothing to
// serve, and from outside is indistinguishable from a bad link.
func TestRespond_UnpublishedSurveyIsNotAnswerable(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("draft-respond"))
	id := app.CreateSurvey(t, creator, "Still a draft", true)
	app.AddQuestion(t, creator, id, "short_text", "Not live yet", nil)

	resp, err := http.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if strings.Contains(body, "Not live yet") {
		t.Error("an unpublished draft leaked its questions to the public link")
	}
}

// TestRespond_DeletedSurveyIsNotAnswerable: soft-deleting pulls the link.
func TestRespond_DeletedSurveyIsNotAnswerable(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("deleted-respond"))
	id := publishedSurvey(t, app, creator, "Doomed", true, [3]string{"short_text", "Question", ""})

	resp := app.PostForm(t, creator, "/surveys/"+id+"/delete", nil)
	resp.Body.Close()

	after, err := http.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	after.Body.Close()
	if after.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a deleted survey", after.StatusCode)
	}
}

// TestPreview_UsesTheRealRendererAndRecordsNothing is M3-T6: the creator
// sees exactly the respondent's page, and no response can come from it.
func TestPreview_UsesTheRealRendererAndRecordsNothing(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("preview"))
	id := app.CreateSurvey(t, creator, "Preview me", true)
	app.AddQuestion(t, creator, id, "long_text", "A draft question", nil)

	page := getBody(t, creator, app.Server.URL+"/surveys/"+id+"/preview")
	if !bodyContains(page, "A draft question") {
		t.Errorf("preview does not render the draft:\n%s", page)
	}
	if !bodyContains(page, "Preview") || !bodyContains(page, "Nothing you enter here is recorded") {
		t.Errorf("preview is not clearly marked:\n%s", page)
	}
	// The same renderer: respondent markup, not the editor's.
	if !bodyContains(page, "respond-form") {
		t.Errorf("preview is not using the respondent renderer:\n%s", page)
	}

	// Submitting the preview records nothing.
	resp := app.PostForm(t, creator, "/surveys/"+id+"/preview", url.Values{})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "Nothing was submitted") {
		t.Errorf("preview submit should state that nothing was recorded:\n%s", body)
	}

	// And the survey is still an unpublished draft with no responses.
	editor := app.SurveyPage(t, creator, id)
	if !bodyContains(editor, "never been published") {
		t.Errorf("preview appears to have published or altered the survey:\n%s", editor)
	}
}

// TestPreview_RequiresOwnership: a preview shows unpublished questions, so
// it is at least as sensitive as the editor.
func TestPreview_RequiresOwnership(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("preview-owner"))
	id := app.CreateSurvey(t, owner, "Private preview", true)
	app.AddQuestion(t, owner, id, "short_text", "Unreleased question", nil)

	intruder := app.Login(t, apptest.UniqueEmail("preview-intruder"))
	resp, err := intruder.Get(app.Server.URL + "/surveys/" + id + "/preview")
	if err != nil {
		t.Fatalf("GET preview: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	if strings.Contains(body, "Unreleased question") {
		t.Error("preview leaked another workspace's draft")
	}

	anonymous, err := http.Get(app.Server.URL + "/surveys/" + id + "/preview")
	if err != nil {
		t.Fatalf("anonymous GET preview: %v", err)
	}
	anonymous.Body.Close()
	if anonymous.Request.URL.Path != "/login" {
		t.Errorf("anonymous preview landed on %s, want /login", anonymous.Request.URL.Path)
	}
}

// TestRespond_NoThirdPartyOrigins is ADR-0006 asserted mechanically: a
// respondent page must reference nothing but this origin.
func TestRespond_NoThirdPartyOrigins(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("origins"))
	id := publishedSurvey(t, app, creator, "First party only", true,
		[3]string{"short_text", "A question", ""})

	page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	for _, external := range findExternalURLs(page) {
		t.Errorf("respondent page references a third-party origin: %s", external)
	}
}

// TestSecurityHeaders covers M4-T7 on both respondent and creator pages.
func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("headers"))
	id := publishedSurvey(t, app, creator, "Headers", true, [3]string{"short_text", "Q", ""})

	resp, err := http.Get(app.Server.URL + "/s/" + id)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	csp := resp.Header.Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'self'", "script-src 'self'", "frame-ancestors 'none'",
		"form-action 'self'", "base-uri 'none'", "object-src 'none'",
	} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %s", want, csp)
		}
	}
	if strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP permits unsafe sources: %s", csp)
	}
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q, want no-referrer (tokened URLs are personal data)", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	// Dynamic pages are never cached: a cached "closed" page outlives a
	// reopen, and answers must not land in disk cache.
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store on dynamic pages", got)
	}
	static, err := http.Get(app.Server.URL + "/static/css/app.css")
	if err != nil {
		t.Fatalf("GET static: %v", err)
	}
	static.Body.Close()
	if got := static.Header.Get("Cache-Control"); !strings.Contains(got, "max-age") {
		t.Errorf("static assets should be cacheable, got Cache-Control %q", got)
	}
	// Development is plain http, so HSTS would pin localhost to HTTPS.
	if got := resp.Header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("development should not send HSTS, got %q", got)
	}
}

func TestSecurityHeaders_HSTSOutsideDevelopment(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{Env: "staging"})

	resp, err := http.Get(app.Server.URL + "/login")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Strict-Transport-Security"); !strings.Contains(got, "max-age=31536000") {
		t.Errorf("staging should send HSTS, got %q", got)
	}
}

// TestRespond_EnhancementScriptIsServed pins the go:embed pattern: the
// paging script must actually ship in the binary. Its absence is invisible
// to every other test (the no-JS path is the fallback by design), which is
// exactly how it went missing once.
func TestRespond_EnhancementScriptIsServed(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})

	resp, err := http.Get(app.Server.URL + "/static/js/respond.js")
	if err != nil {
		t.Fatalf("GET respond.js: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 — is web/static's go:embed missing the js directory?", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want javascript", ct)
	}
	if !strings.Contains(body, "respond-form") {
		t.Errorf("respond.js does not look like the paging script:\n%.200s", body)
	}
}

// TestRespondentPagesAreNoIndex keeps surveys out of search results and
// scrapes (M4-T5).
func TestRespondentPagesAreNoIndex(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("noindex"))
	id := publishedSurvey(t, app, creator, "Not for robots", true, [3]string{"short_text", "Q", ""})

	page := mustGet(t, &http.Client{}, app.Server.URL+"/s/"+id)
	if !bodyContains(page, `name="robots"`) || !bodyContains(page, "noindex") {
		t.Errorf("respondent page lacks a noindex directive:\n%s", page)
	}

	robots := mustGet(t, &http.Client{}, app.Server.URL+"/robots.txt")
	if !strings.Contains(robots, "Disallow: /s/") {
		t.Errorf("robots.txt does not disallow respondent pages:\n%s", robots)
	}
}
