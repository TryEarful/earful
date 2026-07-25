// Package apptest provides the shared test harness for the "application
// edge" testing seam described in SPEC.md's Testing Decisions: boot the
// real server in-process against a real Postgres, and drive it exactly as
// a browser would. Every milestone's tests reuse this harness unchanged.
//
// Isolation model (decided at M2-T1, see docs/testing.md): tests share
// one database and each create their own unique users/workspaces —
// workspace scoping is the product's own isolation boundary, so tests
// parallelize without truncation.
package apptest

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/clock"
	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/email"
	apphttp "github.com/TryEarful/earful/internal/http"
	"github.com/TryEarful/earful/internal/logging"
	"github.com/TryEarful/earful/internal/store"
)

// Options configures the in-process application instance.
type Options struct {
	// DSN reuses an existing migrated database (from NewDB); empty means
	// provision one.
	DSN string
	// Env is APP_ENV for the instance; default "development". Use
	// "staging" to assert production cookie attributes (Secure).
	Env string
	// GoogleIssuer enables Google login against the given OIDC issuer
	// (tests pass an oidctest.Issuer URL).
	GoogleIssuer string
	// BetaMode boots the instance with the private-beta gate on (M12):
	// invite-code signup, password login, no account-creating side doors.
	BetaMode bool
	// AI injects a provider — normally an *ai.Fake with scripted output.
	// Left nil, the instance boots with no AI configured at all, which is
	// how every pre-M5 test runs and is itself the "degrades gracefully
	// when absent" regression proof (story 74).
	AI ai.Provider
	// AIQuota and AIBudgetEUR override the per-workspace daily token cap
	// and the global daily € breaker, so quota-exhaustion paths are
	// reachable without burning a real budget.
	AIQuota     int64
	AIBudgetEUR float64
}

// App is one booted application instance plus the fakes tests observe
// and manipulate: the captured outbox and the injectable clock.
type App struct {
	Server *httptest.Server
	DSN    string
	Emails *email.Capture
	Clock  *clock.Fake
}

// GoogleClientID/GoogleClientSecret are the fixed OAuth client
// credentials App instances use when GoogleIssuer is set; oidctest
// accepts them.
const (
	GoogleClientID     = "earful-test-client"
	GoogleClientSecret = "earful-test-secret"
	// WebhookSecret is the ESP webhook path secret App instances use.
	WebhookSecret = "earful-test-webhook-secret"
)

// New boots the full application (real handler, real Postgres) with a
// fake clock and a capturing email sender, and returns it ready to be
// driven over HTTP.
func New(t *testing.T, opts Options) *App {
	t.Helper()

	dsn := opts.DSN
	if dsn == "" {
		dsn = NewDB(t)
	}
	env := opts.Env
	if env == "" {
		env = config.EnvDevelopment
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("apptest: create pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// The listener exists before Start, so BASE_URL can point at the real
	// address — links in captured emails are directly fetchable.
	srv := httptest.NewUnstartedServer(nil)
	baseURL := "http://" + srv.Listener.Addr().String()

	// AI accounting defaults are deliberately generous. The workspace
	// quota is per-workspace and therefore per-test, but the € breaker
	// sums the whole shared database for the day: a tight default would
	// let one test's usage trip another test's breaker. Tests that want a
	// trip set the limit themselves.
	quota := opts.AIQuota
	if quota == 0 {
		quota = 1_000_000
	}
	budget := opts.AIBudgetEUR
	if budget == 0 {
		budget = 1_000
	}

	cfg := config.Config{
		Env:         env,
		Port:        0,
		DatabaseURL: dsn,
		LogLevel:    slog.LevelError,
		BaseURL:     baseURL,
		EmailSender: "console",
		// Fixed secret so tests can drive the ESP webhook.
		EmailWebhookSecret:     WebhookSecret,
		BetaMode:               opts.BetaMode,
		AIWorkspaceDailyTokens: quota,
		AIDailyBudgetEUR:       budget,
		AICostPer1KTokensEUR:   0.001,
	}

	var google *auth.GoogleOIDC
	if opts.GoogleIssuer != "" {
		google, err = auth.NewGoogleOIDC(context.Background(), opts.GoogleIssuer,
			GoogleClientID, GoogleClientSecret, baseURL+"/auth/google/callback")
		if err != nil {
			t.Fatalf("apptest: oidc discovery against fake issuer: %v", err)
		}
	}

	app := &App{
		Server: srv,
		DSN:    dsn,
		Emails: email.NewCapture(),
		Clock:  clock.NewFake(time.Now()),
	}
	logger := logging.New(slog.LevelError, os.Stderr)
	srv.Config.Handler = apphttp.NewHandler(cfg, logger, apphttp.Deps{
		Pool:   pool,
		Clock:  app.Clock,
		Email:  app.Emails,
		Google: google,
		AI:     opts.AI,
	})
	srv.Start()
	t.Cleanup(srv.Close)
	return app
}

// BrowserClient returns an http.Client with a cookie jar that follows
// redirects — the closest stand-in for a browser.
func (a *App) BrowserClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("apptest: cookie jar: %v", err)
	}
	return &http.Client{Jar: jar}
}

// UniqueEmail returns a fresh address for per-test data isolation.
func UniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%s@example.test", prefix, uuid.NewString()[:8])
}

var magicLinkRe = regexp.MustCompile(`https?://\S+/auth/magic/verify\?token=[A-Za-z0-9_-]+`)

// MagicLinkTo reads the most recent captured email to addr and extracts
// the sign-in link — the test equivalent of opening your inbox.
func (a *App) MagicLinkTo(t *testing.T, addr string) string {
	t.Helper()
	msgs := a.Emails.To(addr)
	if len(msgs) == 0 {
		t.Fatalf("apptest: no captured email to %s", addr)
	}
	link := magicLinkRe.FindString(msgs[len(msgs)-1].Text)
	if link == "" {
		t.Fatalf("apptest: no magic link in email to %s:\n%s", addr, msgs[len(msgs)-1].Text)
	}
	return link
}

// Login drives the full magic-link flow for addr and returns a
// browser-like client holding the resulting session cookie.
func (a *App) Login(t *testing.T, addr string) *http.Client {
	t.Helper()
	client := a.BrowserClient(t)
	a.LoginWithClient(t, client, addr)
	return client
}

// LoginWithClient drives the magic-link flow for addr using an
// already-constructed client, so callers can pre-seed cookies (session
// fixation tests) before authenticating.
func (a *App) LoginWithClient(t *testing.T, client *http.Client, addr string) {
	t.Helper()

	resp, err := client.PostForm(a.Server.URL+"/auth/magic/request", map[string][]string{"email": {addr}})
	if err != nil {
		t.Fatalf("apptest: request magic link: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apptest: magic request status = %d", resp.StatusCode)
	}

	link := a.MagicLinkTo(t, addr)
	token := link[strings.LastIndex(link, "=")+1:]
	resp, err = client.PostForm(a.Server.URL+"/auth/magic/verify", map[string][]string{"token": {token}})
	if err != nil {
		t.Fatalf("apptest: verify magic link: %v", err)
	}
	resp.Body.Close()
	if got, want := resp.Request.URL.Path, "/dashboard"; got != want {
		t.Fatalf("apptest: login landed on %s, want %s", got, want)
	}
}

// MintBetaCode creates one invite code directly in the database — the
// test-side equivalent of `earful beta-codes add` — and returns its
// plaintext (M12).
func (a *App) MintBetaCode(t *testing.T, label string) string {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), a.DSN)
	if err != nil {
		t.Fatalf("apptest: mint pool: %v", err)
	}
	defer pool.Close()
	svc := auth.NewService(pool, a.Clock, a.Emails, a.Server.URL)
	codes, err := svc.MintBetaCodes(context.Background(), 1, label)
	if err != nil {
		t.Fatalf("apptest: mint beta code: %v", err)
	}
	return codes[0]
}

// SignupWithCode drives the invite-code signup form and returns a client
// holding the fresh session (M12). Asserts it lands on the dashboard.
func (a *App) SignupWithCode(t *testing.T, addr, password, code string) *http.Client {
	t.Helper()
	client := a.BrowserClient(t)
	resp, err := client.PostForm(a.Server.URL+"/signup", map[string][]string{
		"email": {addr}, "password": {password}, "code": {code},
	})
	if err != nil {
		t.Fatalf("apptest: signup: %v", err)
	}
	resp.Body.Close()
	if got, want := resp.Request.URL.Path, "/dashboard"; got != want {
		t.Fatalf("apptest: signup landed on %s (status %d), want %s", got, resp.StatusCode, want)
	}
	return client
}

// LoginWithPassword drives the password login form and returns a client
// holding the session (M12). Asserts it lands on the dashboard.
func (a *App) LoginWithPassword(t *testing.T, addr, password string) *http.Client {
	t.Helper()
	client := a.BrowserClient(t)
	resp, err := client.PostForm(a.Server.URL+"/login", map[string][]string{
		"email": {addr}, "password": {password},
	})
	if err != nil {
		t.Fatalf("apptest: password login: %v", err)
	}
	resp.Body.Close()
	if got, want := resp.Request.URL.Path, "/dashboard"; got != want {
		t.Fatalf("apptest: password login landed on %s (status %d), want %s", got, resp.StatusCode, want)
	}
	return client
}

var csrfRe = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

// CSRFToken fetches an authenticated page and extracts the session's
// CSRF token the way a browser form would carry it.
func (a *App) CSRFToken(t *testing.T, client *http.Client) string {
	t.Helper()
	resp, err := client.Get(a.Server.URL + "/account")
	if err != nil {
		t.Fatalf("apptest: get /account: %v", err)
	}
	defer resp.Body.Close()
	body := ReadBody(t, resp)
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("apptest: no _csrf field on /account (status %d)", resp.StatusCode)
	}
	return m[1]
}

// --- survey helpers (M3) -------------------------------------------------
//
// These drive the real endpoints a creator uses, so tests exercise the
// same paths as the browser rather than seeding the database behind the
// application's back.

// PostForm submits a form with the session's CSRF token attached and
// returns the response, leaving the body open for the caller.
func (a *App) PostForm(t *testing.T, client *http.Client, path string, form url.Values) *http.Response {
	t.Helper()
	if form == nil {
		form = url.Values{}
	}
	if form.Get("_csrf") == "" {
		form.Set("_csrf", a.CSRFToken(t, client))
	}
	resp, err := client.PostForm(a.Server.URL+path, form)
	if err != nil {
		t.Fatalf("apptest: POST %s: %v", path, err)
	}
	return resp
}

// CreateSurvey creates a survey through the UI and returns its id.
func (a *App) CreateSurvey(t *testing.T, client *http.Client, title string, anonymous bool) string {
	t.Helper()
	anonymity := "invited"
	if anonymous {
		anonymity = "anonymous"
	}
	resp := a.PostForm(t, client, "/surveys", url.Values{
		"title":     {title},
		"anonymity": {anonymity},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apptest: create survey %q: status %d", title, resp.StatusCode)
	}
	id := strings.TrimPrefix(resp.Request.URL.Path, "/surveys/")
	if id == "" || strings.Contains(id, "/") {
		t.Fatalf("apptest: create survey landed on %s, expected /surveys/{id}", resp.Request.URL.Path)
	}
	return id
}

// AddQuestion adds a question of the given type and returns the page body
// that resulted, so callers can assert on validation messages.
func (a *App) AddQuestion(t *testing.T, client *http.Client, surveyID, qType, text string, extra url.Values) string {
	t.Helper()
	form := url.Values{"type": {qType}, "text": {text}}
	for k, vs := range extra {
		for _, v := range vs {
			form.Add(k, v)
		}
	}
	resp := a.PostForm(t, client, "/surveys/"+surveyID+"/questions", form)
	defer resp.Body.Close()
	return ReadBody(t, resp)
}

// Publish publishes the survey's draft and returns the resulting page.
func (a *App) Publish(t *testing.T, client *http.Client, surveyID string) string {
	t.Helper()
	resp := a.PostForm(t, client, "/surveys/"+surveyID+"/publish", nil)
	defer resp.Body.Close()
	return ReadBody(t, resp)
}

// SurveyPage fetches the editor page for a survey.
func (a *App) SurveyPage(t *testing.T, client *http.Client, surveyID string) string {
	t.Helper()
	resp, err := client.Get(a.Server.URL + "/surveys/" + surveyID)
	if err != nil {
		t.Fatalf("apptest: GET survey page: %v", err)
	}
	defer resp.Body.Close()
	return ReadBody(t, resp)
}

// QuestionIdentities extracts the Question Identity of every question on
// the editor page, in display order. Tests use it to prove identities
// survive rewording (ADR-0001) without reaching into storage.
func (a *App) QuestionIdentities(t *testing.T, client *http.Client, surveyID string) []string {
	t.Helper()
	body := a.SurveyPage(t, client, surveyID)
	matches := questionActionRe.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	var ids []string
	for _, m := range matches {
		if !seen[m[1]] {
			seen[m[1]] = true
			ids = append(ids, m[1])
		}
	}
	return ids
}

var questionActionRe = regexp.MustCompile(`/surveys/[0-9a-f-]+/questions/([0-9a-f-]{36})`)

// ReadBody drains and returns the response body as a string.
func ReadBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("apptest: read body: %v", err)
	}
	return string(b)
}

// NewDB returns a Postgres DSN with all migrations applied. It skips the
// calling test (does not fail) if no database is reachable via
// TEST_DATABASE_URL or DATABASE_URL, so `go test ./...` stays usable
// without Docker; `make test`/`make check`/CI always provision Postgres.
// See docs/testing.md.
func NewDB(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; see docs/testing.md")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("apptest: open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("apptest: database not reachable (%v); see docs/testing.md", err)
	}

	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("apptest: migrate: %v", err)
	}
	return dsn
}

// NewServer boots the application with default options against dsn.
// Retained from M0 for simple page tests; richer tests use New.
func NewServer(t *testing.T, dsn string) *httptest.Server {
	t.Helper()
	return New(t, Options{DSN: dsn}).Server
}
