package http_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/clock"
	"github.com/TryEarful/earful/internal/email"
	"github.com/TryEarful/earful/internal/oidctest"
)

// M12 — the private beta gate (SPEC stories 77-78): one-shot invite
// codes create accounts, email+password signs them in, the email is
// changeable, and the whole loop sends ZERO emails.

func TestBetaSignup_HappyPath(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})
	code := app.MintBetaCode(t, "happy-path")
	addr := apptest.UniqueEmail("beta-signup")

	client := app.SignupWithCode(t, addr, "a-strong-password", code)

	// The account is real: authenticated pages work.
	resp, err := client.Get(app.Server.URL + "/account")
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if !bodyContains(body, addr) {
		t.Errorf("account page missing %s", addr)
	}

	// The invariant that names the milestone: no email was sent.
	if msgs := app.Emails.To(addr); len(msgs) != 0 {
		t.Errorf("signup sent %d emails; the private beta must send none", len(msgs))
	}
}

func TestBetaSignup_CodeIsSingleUse(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})
	code := app.MintBetaCode(t, "single-use")

	app.SignupWithCode(t, apptest.UniqueEmail("first"), "a-strong-password", code)

	// Second use: refused with the uniform message, no account created.
	client := app.BrowserClient(t)
	resp, err := client.PostForm(app.Server.URL+"/signup", map[string][]string{
		"email": {apptest.UniqueEmail("second")}, "password": {"a-strong-password"}, "code": {code},
	})
	if err != nil {
		t.Fatalf("second signup: %v", err)
	}
	body := apptest.ReadBody(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("second use status = %d, want 422", resp.StatusCode)
	}
	if !bodyContains(body, "invite code isn't valid") {
		t.Errorf("second use should get the uniform invalid-code message:\n%s", body)
	}
}

// TestBetaSignup_ConcurrentRace: the used_at IS NULL guard is a database
// fact, so two simultaneous submits of one code produce exactly one
// account no matter how the goroutines interleave.
func TestBetaSignup_ConcurrentRace(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})
	code := app.MintBetaCode(t, "race")

	results := make([]int, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := app.BrowserClient(t)
			resp, err := client.PostForm(app.Server.URL+"/signup", map[string][]string{
				"email":    {apptest.UniqueEmail("race")},
				"password": {"a-strong-password"},
				"code":     {code},
			})
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.Request.URL.Path == "/dashboard" {
				results[i] = 1
			}
		}(i)
	}
	wg.Wait()
	if got := results[0] + results[1]; got != 1 {
		t.Fatalf("one code produced %d accounts, want exactly 1", got)
	}
}

func TestBetaSignup_RejectsBadInput(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})
	used := app.MintBetaCode(t, "to-revoke")
	// Revoke via the service (CLI-equivalent path).
	svc, pool := betaSvc(t, app)
	defer pool.Close()
	rows, err := svc.ListBetaCodes(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, row := range rows {
		if row.Label == "to-revoke" && row.UsedAt == nil && row.RevokedAt == nil {
			if err := svc.RevokeBetaCode(context.Background(), row.ID); err != nil {
				t.Fatalf("revoke: %v", err)
			}
		}
	}

	cases := []struct {
		name, email, password, code, wantMsg string
	}{
		{"garbage code", apptest.UniqueEmail("g"), "a-strong-password", "earful-nope-nope-nope", "invite code isn't valid"},
		{"revoked code", apptest.UniqueEmail("r"), "a-strong-password", used, "invite code isn't valid"},
		{"weak password", apptest.UniqueEmail("w"), "short", "earful-nope-nope-nope", "at least 8 characters"},
		{"bad email", "not-an-email", "a-strong-password", "earful-nope-nope-nope", "look like an email"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := app.BrowserClient(t)
			resp, err := client.PostForm(app.Server.URL+"/signup", map[string][]string{
				"email": {tc.email}, "password": {tc.password}, "code": {tc.code},
			})
			if err != nil {
				t.Fatalf("signup: %v", err)
			}
			body := apptest.ReadBody(t, resp)
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", resp.StatusCode)
			}
			if !bodyContains(body, tc.wantMsg) {
				t.Errorf("missing %q in:\n%s", tc.wantMsg, body)
			}
		})
	}
}

// TestBetaSignup_NoEnumerationOracle: a request WITHOUT a valid invite
// code must not reveal whether an email already has an account. The pre-
// hardening code created the user first and returned "already exists"
// before ever checking the code, so anyone (code or not) could enumerate
// beta membership at will. The code is now validated first, so a bad code
// yields the uniform invalid-code refusal whether or not the email exists.
func TestBetaSignup_NoEnumerationOracle(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})
	code := app.MintBetaCode(t, "enum-oracle")
	registered := apptest.UniqueEmail("registered")
	app.SignupWithCode(t, registered, "a-strong-password", code)

	// Probe both a known and an unknown email with the same invalid code.
	probe := func(email string) (int, string) {
		client := app.BrowserClient(t)
		resp, err := client.PostForm(app.Server.URL+"/signup", map[string][]string{
			"email": {email}, "password": {"a-strong-password"}, "code": {"earful-nope-nope-nope"},
		})
		if err != nil {
			t.Fatalf("signup probe: %v", err)
		}
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		return resp.StatusCode, body
	}

	knownStatus, knownBody := probe(registered)
	unknownStatus, unknownBody := probe(apptest.UniqueEmail("never-seen"))

	if knownStatus != unknownStatus {
		t.Errorf("status leaks account existence: known=%d unknown=%d", knownStatus, unknownStatus)
	}
	if knownStatus != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", knownStatus)
	}
	if !bodyContains(knownBody, "invite code isn't valid") || !bodyContains(unknownBody, "invite code isn't valid") {
		t.Errorf("both bad-code probes should get the uniform invalid-code message")
	}
	if bodyContains(knownBody, "already exists") {
		t.Errorf("bad-code probe of a registered email leaked its existence:\n%s", knownBody)
	}
}

// TestBetaSignup_RateLimited: signup runs bcrypt and writes rows, so it is
// throttled per IP like login. Past the hourly limit, further attempts are
// refused with 429 before any account work — unique emails each time so
// the per-IP limiter (not the per-email one) is what trips.
func TestBetaSignup_RateLimited(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})

	// signupPerIPPerHour is 20 (unexported); 22 attempts guarantees a trip.
	var last int
	for range 22 {
		client := app.BrowserClient(t)
		resp, err := client.PostForm(app.Server.URL+"/signup", map[string][]string{
			"email":    {apptest.UniqueEmail("flood")},
			"password": {"a-strong-password"},
			"code":     {"earful-nope-nope-nope"},
		})
		if err != nil {
			t.Fatalf("signup: %v", err)
		}
		resp.Body.Close()
		last = resp.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("final attempt status = %d, want 429 once the per-IP limit trips", last)
	}
}

func TestPasswordLogin(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})
	code := app.MintBetaCode(t, "login")
	addr := apptest.UniqueEmail("pw-login")
	app.SignupWithCode(t, addr, "a-strong-password", code)

	t.Run("correct password signs in", func(t *testing.T) {
		app.LoginWithPassword(t, addr, "a-strong-password")
	})

	t.Run("wrong password and unknown email are indistinguishable", func(t *testing.T) {
		for _, form := range []map[string][]string{
			{"email": {addr}, "password": {"wrong-password"}},
			{"email": {apptest.UniqueEmail("ghost")}, "password": {"whatever-here"}},
		} {
			client := app.BrowserClient(t)
			resp, err := client.PostForm(app.Server.URL+"/login", form)
			if err != nil {
				t.Fatalf("login: %v", err)
			}
			body := apptest.ReadBody(t, resp)
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", resp.StatusCode)
			}
			if !bodyContains(body, "Invalid email or password.") {
				t.Errorf("expected the one uniform failure message, got:\n%s", body)
			}
		}
	})

	t.Run("per-email attempts rate limited", func(t *testing.T) {
		victim := apptest.UniqueEmail("limited")
		var last int
		for range 12 {
			client := app.BrowserClient(t)
			resp, err := client.PostForm(app.Server.URL+"/login", map[string][]string{
				"email": {victim}, "password": {"wrong"},
			})
			if err != nil {
				t.Fatalf("login: %v", err)
			}
			resp.Body.Close()
			last = resp.StatusCode
		}
		if last != http.StatusTooManyRequests {
			t.Errorf("12th attempt status = %d, want 429", last)
		}
	})
}

// TestBetaMode_ClosesSideDoors: while the gate is up, no path may CREATE
// an account except invite-code signup — otherwise Google (or a magic
// link) strolls straight past the gate.
func TestBetaMode_ClosesSideDoors(t *testing.T) {
	t.Parallel()
	issuer := oidctest.New(t)
	app := apptest.New(t, apptest.Options{BetaMode: true, GoogleIssuer: issuer.URL()})

	t.Run("magic request plays dead", func(t *testing.T) {
		client := app.BrowserClient(t)
		resp, err := client.PostForm(app.Server.URL+"/auth/magic/request",
			map[string][]string{"email": {apptest.UniqueEmail("magic")}})
		if err != nil {
			t.Fatalf("magic request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("magic request status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("google cannot create an account", func(t *testing.T) {
		addr := apptest.UniqueEmail("google-new")
		issuer.SetIdentity("sub-new-"+addr, addr, true)
		client := app.BrowserClient(t)
		resp, err := client.Get(app.Server.URL + "/auth/google/start")
		if err != nil {
			t.Fatalf("google flow: %v", err)
		}
		resp.Body.Close()
		if resp.Request.URL.Path == "/dashboard" {
			t.Fatal("google sign-in created an account past the invite gate")
		}
	})

	t.Run("google still works for existing accounts", func(t *testing.T) {
		addr := apptest.UniqueEmail("google-existing")
		code := app.MintBetaCode(t, "google-existing")
		app.SignupWithCode(t, addr, "a-strong-password", code)

		issuer.SetIdentity("sub-existing-"+addr, addr, true)
		client := app.BrowserClient(t)
		resp, err := client.Get(app.Server.URL + "/auth/google/start")
		if err != nil {
			t.Fatalf("google flow: %v", err)
		}
		resp.Body.Close()
		if resp.Request.URL.Path != "/dashboard" {
			t.Errorf("existing account's google sign-in landed on %s, want /dashboard", resp.Request.URL.Path)
		}
	})
}

func TestEmailChange(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})
	code := app.MintBetaCode(t, "email-change")
	oldAddr := apptest.UniqueEmail("before")
	newAddr := apptest.UniqueEmail("after")
	client := app.SignupWithCode(t, oldAddr, "a-strong-password", code)
	csrf := app.CSRFToken(t, client)

	t.Run("wrong password refused", func(t *testing.T) {
		resp := app.PostForm(t, client, "/account/email", map[string][]string{
			"email": {newAddr}, "password": {"not-the-password"}, "_csrf": {csrf},
		})
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "password isn't right") {
			t.Errorf("wrong password: status %d body:\n%s", resp.StatusCode, body)
		}
	})

	t.Run("happy path, old email stops working", func(t *testing.T) {
		resp := app.PostForm(t, client, "/account/email", map[string][]string{
			"email": {newAddr}, "password": {"a-strong-password"}, "_csrf": {csrf},
		})
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		if !bodyContains(body, "has been changed") {
			t.Fatalf("expected change notice, got:\n%s", body)
		}

		app.LoginWithPassword(t, newAddr, "a-strong-password")

		stale := app.BrowserClient(t)
		resp2, err := stale.PostForm(app.Server.URL+"/login", map[string][]string{
			"email": {oldAddr}, "password": {"a-strong-password"},
		})
		if err != nil {
			t.Fatalf("old-email login: %v", err)
		}
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("old email still logs in (status %d)", resp2.StatusCode)
		}
	})

	t.Run("duplicate email refused", func(t *testing.T) {
		otherCode := app.MintBetaCode(t, "email-dup")
		otherAddr := apptest.UniqueEmail("occupied")
		app.SignupWithCode(t, otherAddr, "a-strong-password", otherCode)

		resp := app.PostForm(t, client, "/account/email", map[string][]string{
			"email": {otherAddr}, "password": {"a-strong-password"}, "_csrf": {csrf},
		})
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnprocessableEntity || !bodyContains(body, "already uses that address") {
			t.Errorf("duplicate: status %d body:\n%s", resp.StatusCode, body)
		}
	})
}

func TestAdminSurface(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{BetaMode: true})
	svc, pool := betaSvc(t, app)
	defer pool.Close()

	adminCode := app.MintBetaCode(t, "admin-self")
	adminAddr := apptest.UniqueEmail("admin")
	adminClient := app.SignupWithCode(t, adminAddr, "a-strong-password", adminCode)

	userCode := app.MintBetaCode(t, "plain-user")
	userAddr := apptest.UniqueEmail("plain")
	userClient := app.SignupWithCode(t, userAddr, "a-strong-password", userCode)

	t.Run("non-admin sees a 404", func(t *testing.T) {
		resp, err := userClient.Get(app.Server.URL + "/admin/beta-codes")
		if err != nil {
			t.Fatalf("admin page: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("non-admin status = %d, want 404 (surface must not exist for them)", resp.StatusCode)
		}
	})

	// Grant via the service — the CLI's exact code path.
	if err := svc.SetSuperAdmin(context.Background(), adminAddr, true); err != nil {
		t.Fatalf("grant: %v", err)
	}

	t.Run("admin mints; plaintext shows once; list never shows it", func(t *testing.T) {
		csrf := app.CSRFToken(t, adminClient)
		resp := app.PostForm(t, adminClient, "/admin/beta-codes", map[string][]string{
			"count": {"2"}, "label": {"minted-via-web"}, "_csrf": {csrf},
		})
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("mint status = %d", resp.StatusCode)
		}
		if strings.Count(body, "earful-") < 2 {
			t.Fatalf("mint response should show 2 plaintext codes:\n%s", body)
		}

		resp2, err := adminClient.Get(app.Server.URL + "/admin/beta-codes")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		listBody := apptest.ReadBody(t, resp2)
		resp2.Body.Close()
		if !bodyContains(listBody, "minted-via-web") {
			t.Errorf("list missing the new label")
		}
		if strings.Contains(listBody, "earful-") {
			t.Errorf("code plaintext leaked into the list view")
		}
	})

	t.Run("password reset revokes sessions, temp password works", func(t *testing.T) {
		csrf := app.CSRFToken(t, adminClient)
		resp := app.PostForm(t, adminClient, "/admin/reset-password", map[string][]string{
			"email": {userAddr}, "_csrf": {csrf},
		})
		body := apptest.ReadBody(t, resp)
		resp.Body.Close()
		temp := ""
		for _, line := range strings.Split(body, "<code>") {
			if strings.HasPrefix(line, "earful-") {
				temp = line[:strings.Index(line, "<")]
				break
			}
		}
		if temp == "" {
			t.Fatalf("no temporary password in reset response:\n%s", body)
		}

		// The user's old session is dead...
		resp2, err := userClient.Get(app.Server.URL + "/account")
		if err != nil {
			t.Fatalf("stale session: %v", err)
		}
		resp2.Body.Close()
		if resp2.Request.URL.Path != "/login" {
			t.Errorf("old session survived the reset (landed on %s)", resp2.Request.URL.Path)
		}
		// ...the old password is dead...
		c := app.BrowserClient(t)
		resp3, err := c.PostForm(app.Server.URL+"/login", map[string][]string{
			"email": {userAddr}, "password": {"a-strong-password"},
		})
		if err != nil {
			t.Fatalf("old password: %v", err)
		}
		resp3.Body.Close()
		if resp3.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("old password still valid (status %d)", resp3.StatusCode)
		}
		// ...and the temporary one signs in.
		app.LoginWithPassword(t, userAddr, temp)
	})
}

// TestNonBeta_GateAbsent: with beta mode off (every existing test's
// default), the signup surface does not exist and the magic-link flow is
// exactly as it was — the whole M2 suite is the regression proof.
func TestNonBeta_GateAbsent(t *testing.T) {
	t.Parallel()
	app := apptest.New(t, apptest.Options{})
	client := app.BrowserClient(t)
	for _, req := range []struct{ method, path string }{
		{"GET", "/signup"}, {"POST", "/signup"},
	} {
		var resp *http.Response
		var err error
		if req.method == "GET" {
			resp, err = client.Get(app.Server.URL + req.path)
		} else {
			resp, err = client.PostForm(app.Server.URL+req.path, nil)
		}
		if err != nil {
			t.Fatalf("%s %s: %v", req.method, req.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s status = %d, want 404 outside beta", req.method, req.path, resp.StatusCode)
		}
	}
}

// betaSvc builds an auth.Service against the app's database — the same
// construction the CLI subcommands use.
func betaSvc(t *testing.T, app *apptest.App) (*auth.Service, *pgxpool.Pool) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), app.DSN)
	if err != nil {
		t.Fatalf("betaSvc pool: %v", err)
	}
	return auth.NewService(pool, clock.NewFake(app.Clock.Now()), email.NewCapture(), app.Server.URL), pool
}
