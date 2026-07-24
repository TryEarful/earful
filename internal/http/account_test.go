package http_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestAccount_DeleteSoftDeletesAndRevokesSession covers SPEC.md story 5's
// M2 half: deletion deactivates the account immediately and kills the
// session. Hard deletion after 30 days is M8-T2's purge job.
func TestAccount_DeleteSoftDeletesAndRevokesSession(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("delete-me")
	client := app.Login(t, addr)
	csrf := app.CSRFToken(t, client)

	resp, err := client.PostForm(app.Server.URL+"/account/delete", map[string][]string{"_csrf": {csrf}})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.Request.URL.Path != "/goodbye" {
		t.Fatalf("delete landed on %s, want /goodbye", resp.Request.URL.Path)
	}
	if !strings.Contains(body, "Your account is deleted") {
		t.Errorf("goodbye page missing confirmation copy:\n%s", body)
	}

	// The session is gone: protected pages bounce to /login.
	resp, err = client.Get(app.Server.URL + "/dashboard")
	if err != nil {
		t.Fatalf("dashboard after delete: %v", err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/login" {
		t.Errorf("dashboard still reachable after account deletion (landed on %s)", resp.Request.URL.Path)
	}
}

// TestAccount_DeletedUserCanSignUpAgain: the partial unique index covers
// live rows only, so the same address may start over immediately while
// the old rows wait out the purge window.
func TestAccount_DeletedUserCanSignUpAgain(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("reborn")

	client := app.Login(t, addr)
	csrf := app.CSRFToken(t, client)
	resp, err := client.PostForm(app.Server.URL+"/account/delete", map[string][]string{"_csrf": {csrf}})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	resp.Body.Close()

	// Same address, brand-new account: the login flow must complete and
	// land on a working dashboard. (That the workspace is genuinely a new
	// one — no surveys carried over — becomes observable at M3, when
	// workspaces have contents.)
	fresh := app.Login(t, addr)
	body := getBody(t, fresh, app.Server.URL+"/dashboard")
	local, _, _ := strings.Cut(addr, "@")
	if !bodyContains(body, local+"'s workspace") {
		t.Errorf("re-registered user has no workspace:\n%s", body)
	}
}

// TestAccount_PageShowsIdentity is the small render check behind the
// delete flow: the account page states who you are and what deletion
// means before you press the button.
func TestAccount_PageShowsIdentity(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("account-page")
	client := app.Login(t, addr)

	body := getBody(t, client, app.Server.URL+"/account")
	if !strings.Contains(body, addr) {
		t.Errorf("account page does not show the signed-in address:\n%s", body)
	}
	if !strings.Contains(body, "30 days") {
		t.Errorf("account page does not explain the 30-day window:\n%s", body)
	}
}

// TestHealthz reports database liveness — the endpoint compose's
// healthcheck and (later) Cloud Monitoring's uptime check both probe.
func TestHealthz(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})

	resp, err := http.Get(app.Server.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if strings.TrimSpace(body) != "ok" {
		t.Errorf("body = %q, want \"ok\"", body)
	}
}
