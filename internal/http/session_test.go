package http_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/apptest"
)

// TestSession_FixationImpossible: the cookie held before login must not
// become valid after login — CreateSession always mints a fresh token, so
// an attacker who plants a pre-login cookie value gains nothing.
func TestSession_FixationImpossible(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("fixation")

	client := app.BrowserClient(t)
	preLoginCookie := "attacker-planted-value"
	setRawCookie(t, client, app.Server.URL, "earful_session", preLoginCookie)

	app.LoginWithClient(t, client, addr)

	found := false
	for _, c := range client.Jar.Cookies(mustParseURL(t, app.Server.URL)) {
		if c.Name == "earful_session" {
			found = true
			if c.Value == preLoginCookie {
				t.Fatal("session cookie after login still equals the attacker-planted pre-login value")
			}
		}
	}
	if !found {
		t.Fatal("no session cookie present after login")
	}
}

// TestSession_LogoutInvalidatesServerSide: logout must destroy the
// session row, not just clear the client cookie — replaying the old
// cookie value must fail.
func TestSession_LogoutInvalidatesServerSide(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("logout")
	client := app.Login(t, addr)

	oldCookie := cookieValue(t, client, app.Server.URL, "earful_session")
	csrf := app.CSRFToken(t, client)

	resp, err := client.PostForm(app.Server.URL+"/logout", map[string][]string{"_csrf": {csrf}})
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/login" {
		t.Fatalf("logout landed on %s, want /login", resp.Request.URL.Path)
	}

	replay := app.BrowserClient(t)
	setRawCookie(t, replay, app.Server.URL, "earful_session", oldCookie)
	resp, err = replay.Get(app.Server.URL + "/dashboard")
	if err != nil {
		t.Fatalf("replay dashboard: %v", err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/login" {
		t.Fatalf("dashboard reachable after logout with old cookie: landed on %s", resp.Request.URL.Path)
	}
}

// TestSession_CookieFlags_Staging: outside development, session cookies
// must carry Secure (PLAN.md M2-T1). httptest.Server here runs plain
// HTTP, so this checks the emitted attribute, not transport enforcement.
func TestSession_CookieFlags_Staging(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{Env: "staging"})
	addr := apptest.UniqueEmail("staging-cookie")

	client := app.BrowserClient(t)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	mustPostForm(t, client, app.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
	link := app.MagicLinkTo(t, addr)
	token := link[strings.LastIndex(link, "=")+1:]
	resp, err := client.PostForm(app.Server.URL+"/auth/magic/verify", map[string][]string{"token": {token}})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	defer resp.Body.Close()

	var setCookie string
	for _, v := range resp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(v, "earful_session=") {
			setCookie = v
		}
	}
	if setCookie == "" {
		t.Fatal("no earful_session Set-Cookie header")
	}
	for _, attr := range []string{"HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(setCookie, attr) {
			t.Errorf("Set-Cookie missing %s: %s", attr, setCookie)
		}
	}
}

// TestSession_CookieFlags_DevelopmentSkipsSecure documents the one
// deliberate exception: local http://localhost has no TLS to require.
func TestSession_CookieFlags_DevelopmentSkipsSecure(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{Env: "development"})
	addr := apptest.UniqueEmail("dev-cookie")

	client := app.BrowserClient(t)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	mustPostForm(t, client, app.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
	link := app.MagicLinkTo(t, addr)
	token := link[strings.LastIndex(link, "=")+1:]
	resp, err := client.PostForm(app.Server.URL+"/auth/magic/verify", map[string][]string{"token": {token}})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	defer resp.Body.Close()

	var setCookie string
	for _, v := range resp.Header.Values("Set-Cookie") {
		if strings.HasPrefix(v, "earful_session=") {
			setCookie = v
		}
	}
	if strings.Contains(setCookie, "Secure") {
		t.Errorf("development cookie should not set Secure: %s", setCookie)
	}
}

// TestCSRF_Matrix is M2-T1's acceptance test: missing, wrong, and valid
// tokens on an authenticated mutation.
func TestCSRF_Matrix(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("csrf-matrix")
	client := app.Login(t, addr)
	validToken := app.CSRFToken(t, client)

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"missing", "", http.StatusForbidden},
		{"wrong", "not-the-right-token", http.StatusForbidden},
		{"valid", validToken, http.StatusSeeOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Use a fresh client per case so a successful logout doesn't
			// invalidate the session out from under a later subtest.
			c := app.Login(t, apptest.UniqueEmail("csrf-matrix-"+tc.name))
			token := tc.token
			if tc.name == "valid" {
				token = app.CSRFToken(t, c)
			}
			c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			resp, err := c.PostForm(app.Server.URL+"/account/delete", map[string][]string{"_csrf": {token}})
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("%s token: status = %d, want %d", tc.name, resp.StatusCode, tc.want)
			}
		})
	}
}

// TestCSRF_CrossSiteRejectedByStdlibLayer: a request whose Sec-Fetch-Site
// declares cross-site is rejected before the handler even runs, by
// net/http's CrossOriginProtection — the baseline wall beneath the
// per-session token.
func TestCSRF_CrossSiteRejectedByStdlibLayer(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	addr := apptest.UniqueEmail("cross-site")
	client := app.Login(t, addr)
	csrf := app.CSRFToken(t, client)

	req, err := http.NewRequest(http.MethodPost, app.Server.URL+"/account/delete",
		strings.NewReader("_csrf="+csrf))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	for _, c := range client.Jar.Cookies(mustParseURL(t, app.Server.URL)) {
		req.AddCookie(c)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site POST status = %d, want 403", resp.StatusCode)
	}
}
