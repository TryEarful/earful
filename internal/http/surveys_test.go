package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestSurvey_CreateAndAppearInList covers SPEC.md stories 6 and 16: a
// survey is created with its anonymity fixed, and shows on the dashboard
// with its status.
func TestSurvey_CreateAndAppearInList(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("create-survey"))

	id := app.CreateSurvey(t, client, "Team retro Q3", true)

	body := getBody(t, client, app.Server.URL+"/surveys")
	if !bodyContains(body, "Team retro Q3") {
		t.Errorf("dashboard does not list the new survey:\n%s", body)
	}
	if !bodyContains(body, "Draft") {
		t.Errorf("a never-published survey should show status Draft:\n%s", body)
	}
	if !bodyContains(body, "Anonymous") {
		t.Errorf("survey list should show the anonymity choice:\n%s", body)
	}

	page := app.SurveyPage(t, client, id)
	if !bodyContains(page, "Team retro Q3") {
		t.Errorf("survey page missing title:\n%s", page)
	}
}

// TestSurvey_AllEightQuestionTypes covers story 8: the editor accepts
// each supported type.
func TestSurvey_AllEightQuestionTypes(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("types"))
	id := app.CreateSurvey(t, client, "Every type", true)

	cases := []struct {
		qType, text string
		extra       url.Values
	}{
		{"long_text", "What stood out this quarter?", nil},
		{"short_text", "Which team are you on?", nil},
		{"single_choice", "Pick one focus area", url.Values{"options": {"Speed\nQuality\nScope"}}},
		{"multiple_choice", "Which tools do you use?", url.Values{"options": {"Go\nPostgres\ntempl"}}},
		{"rating_scale", "Rate the release process", url.Values{"scale_min": {"1"}, "scale_max": {"5"}}},
		{"nps", "How likely are you to recommend us?", nil},
		{"yes_no", "Did you have what you needed?", nil},
		{"dropdown", "Where are you based?", url.Values{"options": {"Amsterdam\nBerlin\nLisbon"}}},
	}
	for _, tc := range cases {
		body := app.AddQuestion(t, client, id, tc.qType, tc.text, tc.extra)
		if !bodyContains(body, tc.text) {
			t.Errorf("%s question was not added:\n%s", tc.qType, body)
		}
	}

	page := app.SurveyPage(t, client, id)
	for _, tc := range cases {
		if !bodyContains(page, tc.text) {
			t.Errorf("editor missing %s question after reload", tc.qType)
		}
	}
}

// TestSurvey_QuestionValidation keeps malformed questions out of a draft,
// with messages a creator can act on.
func TestSurvey_QuestionValidation(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("validation"))
	id := app.CreateSurvey(t, client, "Validation", true)

	cases := []struct {
		name, qType, text string
		extra             url.Values
		wantMsg           string
	}{
		{"empty text", "long_text", "   ", nil, "give the question some text"},
		{"one option", "single_choice", "Pick one", url.Values{"options": {"Only"}}, "at least two options"},
		{"duplicate options", "single_choice", "Pick one", url.Values{"options": {"Yes\nyes"}}, "identical"},
		{"bad scale", "rating_scale", "Rate it", url.Values{"scale_min": {"1"}, "scale_max": {"99"}}, "scale must start"},
		{"unknown type", "telepathy", "Think it", nil, "choose a question type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := app.AddQuestion(t, client, id, tc.qType, tc.text, tc.extra)
			if !bodyContains(body, tc.wantMsg) {
				t.Errorf("expected message %q, got:\n%s", tc.wantMsg, body)
			}
		})
	}

	// None of them made it into the draft.
	if got := app.QuestionIdentities(t, client, id); len(got) != 0 {
		t.Errorf("draft holds %d questions after only invalid submissions", len(got))
	}
}

// TestSurvey_AnonymityIsImmutable covers story 7 across every path the
// application exposes: no form field, no parameter, nothing changes it.
func TestSurvey_AnonymityIsImmutable(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("immutable-anon"))
	id := app.CreateSurvey(t, client, "Anonymous forever", true)

	// Try to flip it through the settings form, which does not offer the
	// field — a hand-crafted POST is exactly what an attacker would send.
	resp := app.PostForm(t, client, "/surveys/"+id+"/settings", url.Values{
		"title":        {"Anonymous forever"},
		"anonymity":    {"invited"},
		"is_anonymous": {"false"},
	})
	resp.Body.Close()

	page := app.SurveyPage(t, client, id)
	if !bodyContains(page, "Anonymous survey") {
		t.Errorf("survey stopped being anonymous after a crafted settings POST:\n%s", page)
	}
	if bodyContains(page, "Invited survey") {
		t.Errorf("survey now presents as invited:\n%s", page)
	}
}

// TestSurvey_PublishFreezesVersion covers stories 11 and 14: publishing
// creates version 1 and moves the survey from Draft to Open.
func TestSurvey_PublishFreezesVersion(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("publish"))
	id := app.CreateSurvey(t, client, "Publishable", true)
	app.AddQuestion(t, client, id, "long_text", "What should we improve?", nil)

	body := app.Publish(t, client, id)
	if !bodyContains(body, "Published version 1") {
		t.Fatalf("publish did not confirm version 1:\n%s", body)
	}
	if !bodyContains(body, "Open") {
		t.Errorf("published survey should show status Open:\n%s", body)
	}
	if !bodyContains(body, "Version 1") {
		t.Errorf("version list missing version 1:\n%s", body)
	}
}

// TestSurvey_PublishRequiresQuestions: an empty survey cannot be
// published, since respondents would have nothing to answer.
func TestSurvey_PublishRequiresQuestions(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("empty-publish"))
	id := app.CreateSurvey(t, client, "Empty", true)

	body := app.Publish(t, client, id)
	if !bodyContains(body, "at least one question") {
		t.Errorf("expected a refusal to publish an empty survey, got:\n%s", body)
	}
	if bodyContains(body, "Published version") {
		t.Error("an empty survey was published")
	}
}

// TestSurvey_RepublishingUnchangedDraftIsRefused keeps the version
// history meaningful: a double-click must not mint a version identical to
// the live one.
func TestSurvey_RepublishingUnchangedDraftIsRefused(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("republish"))
	id := app.CreateSurvey(t, client, "Republish", true)
	app.AddQuestion(t, client, id, "yes_no", "Ready to ship?", nil)

	app.Publish(t, client, id)
	body := app.Publish(t, client, id)
	if !bodyContains(body, "Nothing to publish") {
		t.Errorf("second publish of an unchanged draft should be refused:\n%s", body)
	}
	if bodyContains(body, "Version 2") {
		t.Error("an identical version 2 was created")
	}
}

// TestSurvey_RewordKeepsIdentityNewQuestionGetsNew is the ADR-0001
// contract: reworded questions stay the same question across versions, so
// results remain comparable; a genuinely new question starts a new
// identity.
func TestSurvey_RewordKeepsIdentityNewQuestionGetsNew(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("identity"))
	id := app.CreateSurvey(t, client, "Identity", true)

	app.AddQuestion(t, client, id, "long_text", "How was it?", nil)
	before := app.QuestionIdentities(t, client, id)
	if len(before) != 1 {
		t.Fatalf("expected 1 question, got %d", len(before))
	}
	app.Publish(t, client, id)

	// Reword it substantially: same question, better wording.
	resp := app.PostForm(t, client, "/surveys/"+id+"/questions/"+before[0], url.Values{
		"type": {"long_text"},
		"text": {"Looking back, how did the quarter go for you?"},
	})
	resp.Body.Close()

	after := app.QuestionIdentities(t, client, id)
	if len(after) != 1 || after[0] != before[0] {
		t.Errorf("rewording changed the Question Identity: before %v, after %v", before, after)
	}
	page := app.SurveyPage(t, client, id)
	if !bodyContains(page, "Looking back, how did the quarter go for you?") {
		t.Errorf("reworded text not shown:\n%s", page)
	}

	// A genuinely new question gets its own identity.
	app.AddQuestion(t, client, id, "short_text", "Anything else?", nil)
	withNew := app.QuestionIdentities(t, client, id)
	if len(withNew) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(withNew))
	}
	if withNew[1] == before[0] {
		t.Error("a new question reused an existing Question Identity")
	}
}

// TestSurvey_EditingAfterPublishLeavesLiveVersionAlone covers story 12:
// edits accumulate in the draft while the published version stays live.
func TestSurvey_EditingAfterPublishLeavesLiveVersionAlone(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("edit-after-publish"))
	id := app.CreateSurvey(t, client, "Live edit", true)
	app.AddQuestion(t, client, id, "long_text", "Original wording", nil)
	app.Publish(t, client, id)

	app.AddQuestion(t, client, id, "short_text", "Draft-only question", nil)

	page := app.SurveyPage(t, client, id)
	if !bodyContains(page, "live version 1") {
		t.Errorf("version 1 should still be live while the draft moves ahead:\n%s", page)
	}
	if !bodyContains(page, "Draft-only question") {
		t.Errorf("the draft should hold the new question:\n%s", page)
	}
	if !bodyContains(page, "Publish version 2") {
		t.Errorf("editor should offer publishing version 2:\n%s", page)
	}

	app.Publish(t, client, id)
	page = app.SurveyPage(t, client, id)
	if !bodyContains(page, "Version 2") || !bodyContains(page, "Version 1") {
		t.Errorf("both versions should appear in the history:\n%s", page)
	}
}

// TestSurvey_QuestionReorderAndDelete exercises the remaining draft
// editing operations.
func TestSurvey_QuestionReorderAndDelete(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("reorder"))
	id := app.CreateSurvey(t, client, "Ordering", true)

	app.AddQuestion(t, client, id, "short_text", "First question", nil)
	app.AddQuestion(t, client, id, "short_text", "Second question", nil)
	ids := app.QuestionIdentities(t, client, id)
	if len(ids) != 2 {
		t.Fatalf("expected 2 questions, got %d", len(ids))
	}

	resp := app.PostForm(t, client, "/surveys/"+id+"/questions/"+ids[1]+"/move", url.Values{"direction": {"up"}})
	resp.Body.Close()
	reordered := app.QuestionIdentities(t, client, id)
	if reordered[0] != ids[1] {
		t.Errorf("move up did not reorder: %v then %v", ids, reordered)
	}

	resp = app.PostForm(t, client, "/surveys/"+id+"/questions/"+ids[0]+"/delete", nil)
	resp.Body.Close()
	page := app.SurveyPage(t, client, id)
	if bodyContains(page, "First question") {
		t.Errorf("deleted question still present:\n%s", page)
	}
	if !bodyContains(page, "Second question") {
		t.Errorf("surviving question disappeared:\n%s", page)
	}
}

// TestSurvey_CloseAndReopen covers story 15 and the manual half of
// story 14.
func TestSurvey_CloseAndReopen(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("close-reopen"))
	id := app.CreateSurvey(t, client, "Fieldwork", true)
	app.AddQuestion(t, client, id, "yes_no", "Still going?", nil)
	app.Publish(t, client, id)

	resp := app.PostForm(t, client, "/surveys/"+id+"/close", nil)
	resp.Body.Close()
	page := app.SurveyPage(t, client, id)
	if !bodyContains(page, "This survey is closed") {
		t.Errorf("survey should read as closed:\n%s", page)
	}

	resp = app.PostForm(t, client, "/surveys/"+id+"/reopen", nil)
	resp.Body.Close()
	page = app.SurveyPage(t, client, id)
	if !bodyContains(page, "open and accepting responses") {
		t.Errorf("survey should read as open after reopening:\n%s", page)
	}
}

// TestSurvey_CloseDateClosesAutomatically drives the Close Date through
// the injectable clock: no job runs, the status simply derives from the
// date, so it can never be stale.
func TestSurvey_CloseDateClosesAutomatically(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("close-date"))
	id := app.CreateSurvey(t, client, "Timed", true)
	app.AddQuestion(t, client, id, "yes_no", "In time?", nil)
	app.Publish(t, client, id)

	closeDay := app.Clock.Now().Add(48 * time.Hour).Format("2006-01-02")
	resp := app.PostForm(t, client, "/surveys/"+id+"/settings", url.Values{
		"title":    {"Timed"},
		"close_at": {closeDay},
	})
	resp.Body.Close()

	if page := app.SurveyPage(t, client, id); !bodyContains(page, "open and accepting responses") {
		t.Fatalf("survey should still be open before its close date:\n%s", page)
	}

	app.Clock.Advance(72 * time.Hour)
	page := app.SurveyPage(t, client, id)
	if !bodyContains(page, "closed automatically on its close date") {
		t.Errorf("survey should be closed once the close date passed:\n%s", page)
	}

	// Reopening clears the elapsed date, or the survey would close again
	// the moment it reopened.
	resp = app.PostForm(t, client, "/surveys/"+id+"/reopen", nil)
	resp.Body.Close()
	page = app.SurveyPage(t, client, id)
	if !bodyContains(page, "open and accepting responses") {
		t.Errorf("reopening past a close date did not reopen the survey:\n%s", page)
	}
}

// TestSurvey_AuditLogShowsSavesAndPublishes covers stories 9 and 10.
func TestSurvey_AuditLogShowsSavesAndPublishes(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("audit")
	client := app.Login(t, addr)
	id := app.CreateSurvey(t, client, "Audited", true)

	app.AddQuestion(t, client, id, "short_text", "First", nil)
	app.AddQuestion(t, client, id, "short_text", "Second", nil)
	app.Publish(t, client, id)

	body := getBody(t, client, app.Server.URL+"/surveys/"+id+"/audit")
	if !bodyContains(body, "Published version 1") {
		t.Errorf("audit log missing the publish entry:\n%s", body)
	}
	if !bodyContains(body, "Saved the draft (1 question)") {
		t.Errorf("audit log missing the first save:\n%s", body)
	}
	if !bodyContains(body, "Saved the draft (2 questions)") {
		t.Errorf("audit log missing the second save:\n%s", body)
	}
	if !bodyContains(body, addr) {
		t.Errorf("audit log does not attribute changes to their author:\n%s", body)
	}
}

// TestSurvey_SoftDeleteHidesFromWorkspace covers story 60's creator-facing
// half: a deleted survey leaves the dashboard immediately.
func TestSurvey_SoftDeleteHidesFromWorkspace(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("soft-delete"))
	id := app.CreateSurvey(t, client, "Doomed survey", true)

	resp := app.PostForm(t, client, "/surveys/"+id+"/delete", nil)
	resp.Body.Close()

	list := getBody(t, client, app.Server.URL+"/surveys")
	if bodyContains(list, "Doomed survey") {
		t.Errorf("deleted survey still listed:\n%s", list)
	}

	direct, err := client.Get(app.Server.URL + "/surveys/" + id)
	if err != nil {
		t.Fatalf("GET deleted survey: %v", err)
	}
	direct.Body.Close()
	if direct.StatusCode != http.StatusNotFound {
		t.Errorf("deleted survey still reachable: status %d", direct.StatusCode)
	}
}

// TestSurvey_CrossWorkspaceAccessDenied completes M2-T4's acceptance
// criterion, which needed surveys to exist before it could be proven:
// another workspace's survey is indistinguishable from one that does not
// exist, on every route.
func TestSurvey_CrossWorkspaceAccessDenied(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})

	owner := app.Login(t, apptest.UniqueEmail("owner"))
	id := app.CreateSurvey(t, owner, "Confidential research", true)
	app.AddQuestion(t, owner, id, "long_text", "Secret question", nil)

	intruder := app.Login(t, apptest.UniqueEmail("intruder"))

	t.Run("cannot read", func(t *testing.T) {
		resp, err := intruder.Get(app.Server.URL + "/surveys/" + id)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
		if strings.Contains(body, "Confidential research") || strings.Contains(body, "Secret question") {
			t.Error("another workspace's survey content leaked into the response")
		}
	})

	t.Run("cannot see in list", func(t *testing.T) {
		if body := getBody(t, intruder, app.Server.URL+"/surveys"); bodyContains(body, "Confidential research") {
			t.Error("another workspace's survey appears in the list")
		}
	})

	for _, path := range []string{
		"/surveys/" + id + "/settings",
		"/surveys/" + id + "/publish",
		"/surveys/" + id + "/close",
		"/surveys/" + id + "/delete",
		"/surveys/" + id + "/questions",
	} {
		t.Run("cannot POST "+path, func(t *testing.T) {
			resp := app.PostForm(t, intruder, path, url.Values{"title": {"hijacked"}, "type": {"yes_no"}, "text": {"hijacked"}})
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404", resp.StatusCode)
			}
		})
	}

	// The owner's survey is untouched by any of it.
	page := app.SurveyPage(t, owner, id)
	if !bodyContains(page, "Confidential research") || !bodyContains(page, "Secret question") {
		t.Errorf("owner's survey was altered by the intruder's attempts:\n%s", page)
	}
	if bodyContains(page, "hijacked") {
		t.Error("intruder modified another workspace's survey")
	}
}

// TestSurvey_AuditLogCrossWorkspaceDenied checks the audit route too — a
// trail of someone else's edits is exactly as sensitive as the survey.
func TestSurvey_AuditLogCrossWorkspaceDenied(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	owner := app.Login(t, apptest.UniqueEmail("audit-owner"))
	id := app.CreateSurvey(t, owner, "Private audit", true)

	intruder := app.Login(t, apptest.UniqueEmail("audit-intruder"))
	resp, err := intruder.Get(app.Server.URL + "/surveys/" + id + "/audit")
	if err != nil {
		t.Fatalf("GET audit: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}
