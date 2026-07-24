package http_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestWorkspace_AutoCreatedOnFirstLogin covers SPEC.md story 3: the
// personal workspace exists the moment you first sign in (ADR-0002) — no
// setup step, no empty-state prompt to create one.
func TestWorkspace_AutoCreatedOnFirstLogin(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("autocreate")
	client := app.Login(t, addr)

	resp, err := client.Get(app.Server.URL + "/dashboard")
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()

	local, _, _ := strings.Cut(addr, "@")
	if !bodyContains(body, local+"'s workspace") {
		t.Errorf("dashboard does not show the auto-created workspace:\n%s", body)
	}
}

// TestWorkspace_StableAcrossLogins: signing in again lands in the same
// workspace, not a fresh one. What this can observe at M2 is the name;
// the decisive check — a survey created in one session being present in
// the next — arrives with M3, when workspaces have contents.
func TestWorkspace_StableAcrossLogins(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("reuse")
	local, _, _ := strings.Cut(addr, "@")

	first := app.Login(t, addr)
	second := app.Login(t, addr)

	for i, client := range []*http.Client{first, second} {
		body := getBody(t, client, app.Server.URL+"/dashboard")
		if !bodyContains(body, local+"'s workspace") {
			t.Errorf("login %d: dashboard missing %q:\n%s", i+1, local+"'s workspace", body)
		}
	}
}

// TestWorkspace_UsersSeeOnlyTheirOwn covers SPEC.md story 4 at the level
// M2 can observe it: each user's dashboard shows their workspace and
// never the other's. Resource-level denial (a survey belonging to another
// workspace) gets its own test once M3 introduces surveys.
func TestWorkspace_UsersSeeOnlyTheirOwn(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})

	addrA := apptest.UniqueEmail("tenant-a")
	addrB := apptest.UniqueEmail("tenant-b")
	localA, _, _ := strings.Cut(addrA, "@")
	localB, _, _ := strings.Cut(addrB, "@")

	clientA := app.Login(t, addrA)
	clientB := app.Login(t, addrB)

	bodyA := getBody(t, clientA, app.Server.URL+"/dashboard")
	bodyB := getBody(t, clientB, app.Server.URL+"/dashboard")

	if !bodyContains(bodyA, localA+"'s workspace") {
		t.Errorf("A's dashboard missing A's workspace:\n%s", bodyA)
	}
	if bodyContains(bodyA, localB+"'s workspace") || strings.Contains(bodyA, addrB) {
		t.Errorf("A's dashboard leaks B's workspace or email:\n%s", bodyA)
	}
	if !bodyContains(bodyB, localB+"'s workspace") {
		t.Errorf("B's dashboard missing B's workspace:\n%s", bodyB)
	}
	if bodyContains(bodyB, localA+"'s workspace") || strings.Contains(bodyB, addrA) {
		t.Errorf("B's dashboard leaks A's workspace or email:\n%s", bodyB)
	}
}

// TestAuth_ProtectedPagesRequireSession: every authenticated route sends
// anonymous visitors to /login rather than rendering anything.
func TestAuth_ProtectedPagesRequireSession(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})

	for _, path := range []string{"/dashboard", "/account"} {
		resp, err := http.Get(app.Server.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		if resp.Request.URL.Path != "/login" {
			t.Errorf("GET %s (anonymous) landed on %s, want /login", path, resp.Request.URL.Path)
		}
		if !strings.Contains(body, "Sign in") {
			t.Errorf("GET %s (anonymous) did not render the sign-in page", path)
		}
	}
}

// TestLogin_RedirectsWhenAlreadySignedIn keeps the signed-in experience
// coherent: /login is not a dead end for people who already have a
// session.
func TestLogin_RedirectsWhenAlreadySignedIn(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.Login(t, apptest.UniqueEmail("already-in"))

	resp, err := client.Get(app.Server.URL + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/dashboard" {
		t.Errorf("signed-in /login landed on %s, want /dashboard", resp.Request.URL.Path)
	}
}

func getBody(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	return apptest.ReadBody(t, resp)
}
