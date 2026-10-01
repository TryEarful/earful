package http_test

import (
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

var workspaceFieldRe = regexp.MustCompile(`name="workspace" value="([0-9a-f-]{36})"`)

// TestAITiers_SuperAdminRaisesAWorkspacesAllowance is issue #3 end to
// end: a workspace that has spent the normal allowance is refused, a
// super admin moves it to the high tier, and the same day it may use AI
// again. Nobody else can see or use the control.
func TestAITiers_SuperAdminRaisesAWorkspacesAllowance(t *testing.T) {
	t.Parallel()
	fake := generatorFake()
	app := apptest.New(t, apptest.Options{AI: fake, AIQuota: 1, AIHighQuota: 1_000_000})
	address := apptest.UniqueEmail("tiered")
	creator := app.Login(t, address)
	id := app.CreateSurvey(t, creator, "Tiers", true)

	app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{"prompt": {"first"}}).Body.Close()
	resp := app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{"prompt": {"second"}})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "AI allowance. It resets tomorrow") {
		t.Fatalf("the normal tier's cap did not refuse:\n%s", body)
	}

	// The account page shows where the workspace stands.
	account := mustGet(t, creator, app.Server.URL+"/account")
	if !bodyContains(account, "AI today") || !bodyContains(account, "of 1 tokens · Tier: Normal") {
		t.Errorf("the account page does not show today's AI usage and tier:\n%s", account)
	}

	// A creator cannot see the control, or use it.
	for _, req := range []func() *http.Response{
		func() *http.Response {
			r, err := creator.Get(app.Server.URL + "/admin/ai-tiers")
			if err != nil {
				t.Fatalf("GET as creator: %v", err)
			}
			return r
		},
		func() *http.Response {
			return app.PostForm(t, creator, "/admin/ai-tiers", url.Values{"tier": {"high"}, "workspace": {"00000000-0000-0000-0000-000000000000"}})
		},
	} {
		r := req()
		r.Body.Close()
		if r.StatusCode != http.StatusNotFound {
			t.Errorf("creator got status %d, want 404", r.StatusCode)
		}
	}

	admin := app.LoginAsSuperAdmin(t, apptest.UniqueEmail("tieradmin"))
	page := mustGet(t, admin, app.Server.URL+"/admin/ai-tiers?email="+url.QueryEscape(address))
	if !bodyContains(page, "Used today:") || !bodyContains(page, "High, 1,000,000 tokens a day") {
		t.Fatalf("lookup does not show the workspace and its choices:\n%s", page)
	}
	m := workspaceFieldRe.FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no workspace on the lookup:\n%s", page)
	}
	workspace := m[1]

	// A tier the database does not know is refused before it gets there.
	resp = app.PostForm(t, admin, "/admin/ai-tiers", url.Values{
		"email": {address}, "workspace": {workspace}, "tier": {"unlimited"},
	})
	body = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "Choose one of the tiers listed.") {
		t.Errorf("an unknown tier: status %d\n%s", resp.StatusCode, body)
	}

	// A change without the session's token is refused.
	resp, err := admin.PostForm(app.Server.URL+"/admin/ai-tiers", url.Values{
		"email": {address}, "workspace": {workspace}, "tier": {"high"},
	})
	if err != nil {
		t.Fatalf("POST without token: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("a change without a CSRF token got %d, want 403", resp.StatusCode)
	}

	resp = app.PostForm(t, admin, "/admin/ai-tiers", url.Values{
		"email": {address}, "workspace": {workspace}, "tier": {"high"},
	})
	body = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, "Tier saved.") {
		t.Fatalf("the change was not confirmed (status %d):\n%s", resp.StatusCode, body)
	}
	if !regexp.MustCompile(`<option value="high" selected`).MatchString(body) {
		t.Errorf("the saved tier is not the one selected:\n%s", body)
	}

	// The same day, the workspace may use AI again.
	resp = app.PostForm(t, creator, "/surveys/"+id+"/generate", url.Values{"prompt": {"third"}})
	body = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if bodyContains(body, "AI allowance. It resets tomorrow") || len(fake.GenerateCalls) != 2 {
		t.Errorf("after moving to high, generate calls = %d; still refused:\n%s", len(fake.GenerateCalls), body)
	}
	if account := mustGet(t, creator, app.Server.URL+"/account"); !bodyContains(account, "of 1,000,000 tokens · Tier: High") {
		t.Errorf("the account page does not show the new tier:\n%s", account)
	}
}

// TestAITiers_UnknownWorkspace: a workspace that has gone is said so,
// not an error page.
func TestAITiers_UnknownWorkspace(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	admin := app.LoginAsSuperAdmin(t, apptest.UniqueEmail("tieradmin"))
	resp := app.PostForm(t, admin, "/admin/ai-tiers", url.Values{
		"workspace": {"00000000-0000-0000-0000-000000000000"}, "tier": {"high"},
	})
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "That workspace no longer exists.") {
		t.Errorf("unknown workspace: status %d\n%s", resp.StatusCode, body)
	}

	// An address with no account finds nothing, and says so.
	page := mustGet(t, admin, app.Server.URL+"/admin/ai-tiers?email="+url.QueryEscape(apptest.UniqueEmail("nobody")))
	if !bodyContains(page, "No workspace found for") {
		t.Errorf("an unknown address did not say so:\n%s", page)
	}
}
