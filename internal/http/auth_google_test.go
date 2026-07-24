package http_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/oidctest"
)

// TestGoogleLogin_FullFlow covers SPEC.md story 1: the whole
// authorization-code dance against the fake issuer (real discovery, real
// JWKS fetch, real RS256 verification on our side), ending signed-in with
// the personal workspace created.
func TestGoogleLogin_FullFlow(t *testing.T) {
	t.Parallel()
	issuer := oidctest.New(t)
	app := apptest.New(t, apptest.Options{GoogleIssuer: issuer.URL()})
	addr := apptest.UniqueEmail("google-flow")
	issuer.SetIdentity("google-sub-1-"+addr, addr, true)

	client := app.BrowserClient(t)
	// One GET: /auth/google/start → issuer /authorize (auto-approves) →
	// /auth/google/callback → /dashboard.
	resp, err := client.Get(app.Server.URL + "/auth/google/start")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.Request.URL.Path != "/dashboard" {
		t.Fatalf("landed on %s, want /dashboard", resp.Request.URL.Path)
	}
	local, _, _ := strings.Cut(addr, "@")
	if !bodyContains(body, local+"'s workspace") {
		t.Errorf("dashboard missing workspace for %s:\n%s", addr, body)
	}
}

// TestGoogleLogin_StateMismatch is M2's CSRF test for the OAuth leg: a
// callback whose state does not match the browser's cookie is refused.
func TestGoogleLogin_StateMismatch(t *testing.T) {
	t.Parallel()
	issuer := oidctest.New(t)
	app := apptest.New(t, apptest.Options{GoogleIssuer: issuer.URL()})

	jarClient := app.BrowserClient(t)
	jarClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := jarClient.Get(app.Server.URL + "/auth/google/start")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse authorize location: %v", err)
	}
	nonce := loc.Query().Get("nonce")

	code := issuer.MintCode(nonce, apptest.GoogleClientID)
	cb := app.Server.URL + "/auth/google/callback?code=" + url.QueryEscape(code) + "&state=tampered-state"
	resp, err = jarClient.Get(cb) // jar still holds the genuine state cookie
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("tampered state: status %d, want 403", resp.StatusCode)
	}
	if !strings.Contains(body, "state mismatch") {
		t.Errorf("expected state-mismatch page, got:\n%s", body)
	}
}

// TestGoogleLogin_BackfillsExistingEmailAccount: an account created via
// magic link later signs in with Google using the same address — same
// account, not a duplicate (observable: no conflict, and a subsequent
// Google login with a changed email still resolves by subject to the
// original account/email).
func TestGoogleLogin_BackfillsExistingEmailAccount(t *testing.T) {
	t.Parallel()
	issuer := oidctest.New(t)
	app := apptest.New(t, apptest.Options{GoogleIssuer: issuer.URL()})
	addr := apptest.UniqueEmail("backfill")
	sub := "google-sub-backfill-" + addr

	app.Login(t, addr) // magic-link account exists first

	issuer.SetIdentity(sub, addr, true)
	client := app.BrowserClient(t)
	resp, err := client.Get(app.Server.URL + "/auth/google/start")
	if err != nil {
		t.Fatalf("google login: %v", err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/dashboard" {
		t.Fatalf("google login for existing email landed on %s, want /dashboard (duplicate account?)", resp.Request.URL.Path)
	}

	// Same subject, email changed on Google's side: still the original
	// account — the dashboard shows the address we know the user by.
	issuer.SetIdentity(sub, apptest.UniqueEmail("changed"), true)
	client2 := app.BrowserClient(t)
	resp, err = client2.Get(app.Server.URL + "/auth/google/start")
	if err != nil {
		t.Fatalf("second google login: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !strings.Contains(body, addr) {
		t.Errorf("subject should resolve to original account %s, got:\n%s", addr, body)
	}
}

// TestGoogleLogin_UnverifiedEmailRejected: an unverified Google email
// must not become an account.
func TestGoogleLogin_UnverifiedEmailRejected(t *testing.T) {
	t.Parallel()
	issuer := oidctest.New(t)
	app := apptest.New(t, apptest.Options{GoogleIssuer: issuer.URL()})
	issuer.SetIdentity("sub-unverified", apptest.UniqueEmail("unverified"), false)

	client := app.BrowserClient(t)
	resp, err := client.Get(app.Server.URL + "/auth/google/start")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	resp.Body.Close()
	if got := resp.Request.URL.Path; got != "/login" {
		t.Fatalf("unverified email landed on %s, want /login", got)
	}
	// No session was created.
	resp, err = client.Get(app.Server.URL + "/dashboard")
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/login" {
		t.Errorf("unverified login produced a session")
	}
}

// TestGoogleLogin_HiddenWhenUnconfigured: self-hosters without OIDC
// credentials get a magic-link-only login page and 404s on the Google
// routes (Appendix D).
func TestGoogleLogin_HiddenWhenUnconfigured(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{}) // no GoogleIssuer

	resp, err := http.Get(app.Server.URL + "/login")
	if err != nil {
		t.Fatalf("login page: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if strings.Contains(body, "Continue with Google") {
		t.Errorf("login page offers Google despite no configuration")
	}

	resp, err = http.Get(app.Server.URL + "/auth/google/start")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("google start status = %d, want 404", resp.StatusCode)
	}
}
