package http_test

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/apptest"
)

// A survey started from a description (issue #20): the new-survey form
// takes an optional description, and the survey opens in the editor with
// the drafted questions already in its draft.

// titledFake answers with a title line ahead of generatedNDJSON, as a
// model does when the system prompt asks for a title.
func titledFake() *ai.Fake {
	output := `{"title":"  First week   with us "}` + "\n" + generatedNDJSON
	return &ai.Fake{GenerateScript: [][]string{splitEvery(output, 29)}}
}

func createFromPrompt(t *testing.T, app *apptest.App, client *http.Client, form url.Values) (*http.Response, string) {
	t.Helper()
	if form.Get("anonymity") == "" {
		form.Set("anonymity", "anonymous")
	}
	resp := app.PostForm(t, client, "/surveys", form)
	defer resp.Body.Close()
	return resp, apptest.ReadBody(t, resp)
}

func TestCreateFromPrompt_DraftsTitleAndQuestions(t *testing.T) {
	t.Parallel()
	fake := titledFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("fromprompt"))

	if page := mustGet(t, creator, app.Server.URL+"/surveys/new"); !bodyContains(page, "Describe your survey") ||
		!strings.Contains(page, `name="prompt"`) {
		t.Fatalf("the new-survey page does not offer a description with AI configured:\n%s", page)
	}

	resp, body := createFromPrompt(t, app, creator, url.Values{
		"title":  {""},
		"prompt": {"the first week of using our product"},
	})
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Request.URL.Path, "/surveys/") {
		t.Fatalf("landed on %s with %d, want the editor", resp.Request.URL, resp.StatusCode)
	}
	id := strings.TrimPrefix(resp.Request.URL.Path, "/surveys/")

	// The model's title, with its whitespace tidied, names the survey.
	if !bodyContains(body, "<h1>First week with us</h1>") {
		t.Errorf("the proposed title did not name the survey:\n%s", body)
	}
	// The title line is neither a question nor a skipped one.
	if !bodyContains(body, "Added 4 questions") || !bodyContains(body, "2 were skipped") {
		t.Errorf("the editor does not say what the run did:\n%s", body)
	}
	if bodyContains(body, "What are you thinking?") {
		t.Error("a question of an invented type reached the draft")
	}

	if len(fake.GenerateCalls) != 1 {
		t.Fatalf("model calls = %d, want 1", len(fake.GenerateCalls))
	}
	call := fake.GenerateCalls[0]
	if !strings.Contains(call.Prompt, "first week") || !strings.Contains(call.System, `{"title":`) {
		t.Errorf("the model was not asked for a title and questions:\nsystem: %s\nprompt: %s", call.System, call.Prompt)
	}

	// The questions are ordinary draft content: edited like any other.
	identities := app.QuestionIdentities(t, creator, id)
	if len(identities) != 4 {
		t.Fatalf("draft holds %d questions, want 4", len(identities))
	}
	edit := app.PostForm(t, creator, "/surveys/"+id+"/questions/"+identities[0], url.Values{
		"type": {"long_text"}, "text": {"Reworded by hand"},
	})
	edit.Body.Close()
	if page := app.SurveyPage(t, creator, id); !bodyContains(page, "Reworded by hand") {
		t.Error("a drafted question could not be edited like any other")
	}
}

func TestCreateFromPrompt_KeepsATypedTitle(t *testing.T) {
	t.Parallel()
	fake := titledFake()
	app := apptest.New(t, apptest.Options{AI: fake})
	creator := app.Login(t, apptest.UniqueEmail("frompromptitle"))

	_, body := createFromPrompt(t, app, creator, url.Values{
		"title":     {"Support follow up"},
		"prompt":    {"what to ask after a support call"},
		"anonymity": {"invited"},
	})
	if !bodyContains(body, "<h1>Support follow up</h1>") {
		t.Errorf("the typed title was not kept:\n%s", body)
	}
	if !bodyContains(body, "Invited survey") {
		t.Errorf("the chosen audience was not kept:\n%s", body)
	}
	if len(fake.GenerateCalls) != 1 || strings.Contains(fake.GenerateCalls[0].System, `{"title":`) {
		t.Errorf("a title was asked for although one was typed")
	}
}

// A reply with no usable title line still creates the survey, under a
// default title, rather than refusing a run that produced questions.
func TestCreateFromPrompt_FallsBackToADefaultTitle(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: generatorFake()})
	creator := app.Login(t, apptest.UniqueEmail("frompromptdefault"))

	_, body := createFromPrompt(t, app, creator, url.Values{"prompt": {"onboarding"}})
	if !bodyContains(body, "<h1>Untitled survey</h1>") || !bodyContains(body, "Added 4 questions") {
		t.Errorf("expected the default title and the drafted questions:\n%s", body)
	}
}

// A refused run creates nothing and hands the form back as typed, with
// the editor's own refusal wording (stories 21, 67).
func TestCreateFromPrompt_QuotaCreatesNothing(t *testing.T) {
	t.Parallel()
	fake := titledFake()
	app := apptest.New(t, apptest.Options{AI: fake, AIQuota: 1})
	creator := app.Login(t, apptest.UniqueEmail("frompromptquota"))

	// The first run spends the allowance.
	createFromPrompt(t, app, creator, url.Values{"prompt": {"first"}})

	resp, body := createFromPrompt(t, app, creator, url.Values{
		"title":  {"Refused by the quota"},
		"prompt": {"a second description"},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity || resp.Request.URL.Path != "/surveys" {
		t.Errorf("status %d at %s, want the form back with 422", resp.StatusCode, resp.Request.URL.Path)
	}
	if !bodyContains(body, "AI allowance. It resets tomorrow") {
		t.Errorf("quota refusal is not readable:\n%s", body)
	}
	if !bodyContains(body, "a second description") || !bodyContains(body, `value="Refused by the quota"`) {
		t.Errorf("the form did not come back as typed:\n%s", body)
	}
	if len(fake.GenerateCalls) != 1 {
		t.Errorf("model calls = %d; a refused request must not reach the provider", len(fake.GenerateCalls))
	}
	if dashboard := mustGet(t, creator, app.Server.URL+"/dashboard"); bodyContains(dashboard, "Refused by the quota") {
		t.Error("a refused run left a survey behind")
	}
}

func TestCreateFromPrompt_ProviderFailureCreatesNothing(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{AI: &ai.Fake{Err: errors.New("provider down")}})
	creator := app.Login(t, apptest.UniqueEmail("frompromptdown"))

	resp, body := createFromPrompt(t, app, creator, url.Values{
		"title":  {"Never created"},
		"prompt": {"anything"},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "didn't respond") {
		t.Errorf("status %d; want the form back with the editor's refusal:\n%s", resp.StatusCode, body)
	}
	if dashboard := mustGet(t, creator, app.Server.URL+"/dashboard"); bodyContains(dashboard, "Never created") {
		t.Error("a failed run left a survey behind")
	}
}

// Without text AI the description is absent, the title is required
// again, and a posted description is ignored (Appendix D).
func TestCreateFromPrompt_AbsentWithoutAProvider(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	creator := app.Login(t, apptest.UniqueEmail("frompromptnoai"))

	page := mustGet(t, creator, app.Server.URL+"/surveys/new")
	if bodyContains(page, "Describe your survey") || strings.Contains(page, `name="prompt"`) {
		t.Errorf("a description was offered with no provider configured:\n%s", page)
	}
	if !strings.Contains(page, `name="title" value="" required`) {
		t.Errorf("the title is not required without AI:\n%s", page)
	}

	resp, _ := createFromPrompt(t, app, creator, url.Values{"prompt": {"anything"}})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("an untitled survey with no AI: status %d, want 422", resp.StatusCode)
	}

	resp, body := createFromPrompt(t, app, creator, url.Values{"title": {"By hand"}, "prompt": {"anything"}})
	if !strings.HasPrefix(resp.Request.URL.Path, "/surveys/") || !bodyContains(body, "<h1>By hand</h1>") {
		t.Fatalf("a titled survey was not created as usual: %s", resp.Request.URL)
	}
	id := strings.TrimPrefix(resp.Request.URL.Path, "/surveys/")
	if n := len(app.QuestionIdentities(t, creator, id)); n != 0 {
		t.Errorf("draft holds %d questions with no AI configured", n)
	}
}
