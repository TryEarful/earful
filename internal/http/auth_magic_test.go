package http_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestMagicLink_FullFlow covers SPEC.md stories 2 and 3: email → link →
// confirm → signed in, with the personal workspace auto-created.
func TestMagicLink_FullFlow(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("magic-flow")
	client := app.BrowserClient(t)

	resp, err := client.PostForm(app.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Check your email") {
		t.Fatalf("magic request: status %d, body %q", resp.StatusCode, body)
	}

	// The emailed link is a GET that only shows a confirmation page.
	link := app.MagicLinkTo(t, addr)
	resp, err = client.Get(link)
	if err != nil {
		t.Fatalf("open link: %v", err)
	}
	body = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Confirm sign-in") || !strings.Contains(body, addr) {
		t.Fatalf("confirm page: status %d, body %q", resp.StatusCode, body)
	}

	// The explicit POST signs in and lands on the dashboard.
	token := link[strings.LastIndex(link, "=")+1:]
	resp, err = client.PostForm(app.Server.URL+"/auth/magic/verify", map[string][]string{"token": {token}})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	body = apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.Request.URL.Path != "/dashboard" {
		t.Fatalf("landed on %s, want /dashboard", resp.Request.URL.Path)
	}
	local, _, _ := strings.Cut(addr, "@")
	if !bodyContains(body, local+"'s workspace") {
		t.Errorf("dashboard missing auto-created workspace name %q:\n%s", local+"'s workspace", body)
	}
}

// TestMagicLink_ScannerPrefetchDoesNotConsume: corporate mail scanners GET
// every link; only the human's POST may consume the single-use token.
func TestMagicLink_ScannerPrefetchDoesNotConsume(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("scanner")
	client := app.BrowserClient(t)

	mustPostForm(t, client, app.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
	link := app.MagicLinkTo(t, addr)

	for range 5 { // scanner hammering the link
		resp, err := http.Get(link)
		if err != nil {
			t.Fatalf("prefetch: %v", err)
		}
		resp.Body.Close()
	}

	token := link[strings.LastIndex(link, "=")+1:]
	resp, err := client.PostForm(app.Server.URL+"/auth/magic/verify", map[string][]string{"token": {token}})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/dashboard" {
		t.Fatalf("login after prefetches landed on %s, want /dashboard", resp.Request.URL.Path)
	}
}

// TestMagicLink_ReplayRejected is M2-T3's replay acceptance test: a
// consumed token never signs in again.
func TestMagicLink_ReplayRejected(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("replay")

	app.Login(t, addr) // consumes the token
	link := app.MagicLinkTo(t, addr)
	token := link[strings.LastIndex(link, "=")+1:]

	attacker := app.BrowserClient(t)
	resp, err := attacker.PostForm(app.Server.URL+"/auth/magic/verify", map[string][]string{"token": {token}})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("replay status = %d, want 400", resp.StatusCode)
	}
	if !strings.Contains(body, "Sign-in link problem") {
		t.Errorf("replay should render the link-problem page, got:\n%s", body)
	}
	// And the replay must not have produced a session.
	resp, err = attacker.Get(app.Server.URL + "/dashboard")
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/login" {
		t.Fatalf("replay attacker reached %s, want /login redirect", resp.Request.URL.Path)
	}
}

// TestMagicLink_Expiry is M2-T3's expiry acceptance test, driven by the
// injectable clock: 15 minutes is a hard wall.
func TestMagicLink_Expiry(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("expiry")
	client := app.BrowserClient(t)

	mustPostForm(t, client, app.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
	link := app.MagicLinkTo(t, addr)
	token := link[strings.LastIndex(link, "=")+1:]

	app.Clock.Advance(16 * time.Minute)

	resp, err := client.Get(link)
	if err != nil {
		t.Fatalf("open expired link: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "expired") {
		t.Fatalf("expired GET: status %d, body %q", resp.StatusCode, body)
	}

	resp, err = client.PostForm(app.Server.URL+"/auth/magic/verify", map[string][]string{"token": {token}})
	if err != nil {
		t.Fatalf("post expired: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expired POST status = %d, want 400", resp.StatusCode)
	}
}

// TestMagicLink_PerEmailRateLimit: the 6th link for one address within an
// hour is refused (database-backed, restart-proof).
func TestMagicLink_PerEmailRateLimit(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("email-limit")
	client := app.BrowserClient(t)

	for i := 0; i < 5; i++ {
		mustPostForm(t, client, app.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
	}
	resp, err := client.PostForm(app.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
	if err != nil {
		t.Fatalf("6th request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("6th request status = %d, want 429", resp.StatusCode)
	}

	// The window slides: an hour later the address may try again.
	app.Clock.Advance(61 * time.Minute)
	mustPostForm(t, client, app.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
}

// TestMagicLink_PerIPRateLimit: one address per request, one IP hammering
// — the in-memory per-IP bucket closes at 10/hour. Runs with a
// staging-shaped instance so X-Forwarded-For (rightmost entry) is
// honored, as behind Cloud Run.
func TestMagicLink_PerIPRateLimit(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{Env: "staging"})

	post := func(ip string) int {
		req, err := http.NewRequest(http.MethodPost, app.Server.URL+"/auth/magic/request",
			strings.NewReader("email="+apptest.UniqueEmail("ip-limit")))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Forwarded-For", "spoofed.example, "+ip)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	for i := 0; i < 10; i++ {
		if got := post("203.0.113.9"); got != http.StatusOK {
			t.Fatalf("request %d from limited IP: status %d, want 200", i+1, got)
		}
	}
	if got := post("203.0.113.9"); got != http.StatusTooManyRequests {
		t.Fatalf("11th request from same IP: status %d, want 429", got)
	}
	// A different client IP is unaffected.
	if got := post("203.0.113.77"); got != http.StatusOK {
		t.Fatalf("request from fresh IP: status %d, want 200", got)
	}
}

// TestMagicLink_InvalidEmailRejected keeps garbage out of the outbox.
func TestMagicLink_InvalidEmailRejected(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.BrowserClient(t)

	resp, err := client.PostForm(app.Server.URL+"/auth/magic/request", map[string][]string{"email": {"not-an-address"}})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	if !bodyContains(body, "doesn't look like an email address") {
		t.Errorf("expected inline validation message, got:\n%s", body)
	}
	if got := len(app.Emails.All()); got != 0 {
		t.Errorf("no email should have been sent, got %d", got)
	}
}

// TestMagicLink_NoAccountEnumeration: the response is the same shape
// whether or not the address has an account — there is nothing to probe.
func TestMagicLink_NoAccountEnumeration(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	existing := apptest.UniqueEmail("enum-existing")
	app.Login(t, existing) // now an account exists for it

	fresh := apptest.UniqueEmail("enum-fresh")
	client := app.BrowserClient(t)
	for _, addr := range []string{existing, fresh} {
		resp, err := client.PostForm(app.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
		if err != nil {
			t.Fatalf("post %s: %v", addr, err)
		}
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, "Check your email") {
			t.Fatalf("response for %s differs: status %d", addr, resp.StatusCode)
		}
	}
}

func mustPostForm(t *testing.T, client *http.Client, url string, form map[string][]string) {
	t.Helper()
	resp, err := client.PostForm(url, form)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("post %s: status %d, want 200", url, resp.StatusCode)
	}
}
