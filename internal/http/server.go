// Package http assembles the application edge: real routing, real
// middleware, real templ rendering, behind one http.Handler. This is the
// seam SPEC.md's "Testing Decisions" describes -- tests wrap NewHandler's
// output in httptest.NewServer and drive it exactly as a browser would.
//
// Callers that also import net/http must alias one of the two imports,
// e.g. apphttp "github.com/TryEarful/earful/internal/http".
package http

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/antibot"
	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/clock"
	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/email"
	"github.com/TryEarful/earful/internal/invites"
	"github.com/TryEarful/earful/internal/store"
)

// Deps carries the runtime dependencies of the handler. Pool is required;
// nil Clock and Email fall back to the real clock and the console sender
// (the local-dev defaults). Google may be nil — Google login is optional
// (self-hosters, Appendix D) and the login page adapts. AI defaults to
// the config-built provider (ai.FromConfig); tests inject an ai.Fake.
type Deps struct {
	Pool   *pgxpool.Pool
	Clock  clock.Clock
	Email  email.Sender
	Google *auth.GoogleOIDC
	AI     ai.Provider
}

type server struct {
	cfg         config.Config
	logger      *slog.Logger
	pool        *pgxpool.Pool
	clock       clock.Clock
	auth        *auth.Service
	surveys     *store.Surveys
	invites     *invites.Service
	emailSender email.Sender
	google      *auth.GoogleOIDC
	// ai and aiMeter are wired but not yet reached by any handler. When an
	// AI operation IS wired up, every ai.Provider call MUST be preceded by
	// aiMeter.Check (the per-workspace token cap + global daily € breaker);
	// TestAIProviderCallsAreMetered fails the build if one isn't.
	ai      ai.Provider
	aiMeter *ai.Meter

	// health caches the DB liveness probe so a flood of unauthenticated
	// /health hits cannot turn into an unbounded stream of DB round-trips.
	healthMu        sync.Mutex
	healthCheckedAt time.Time
	healthOK        bool

	// Anti-abuse (M4-T5). All in-memory and per-process by design; see
	// the antibot package for the trade-off notes.
	challenges *antibot.Challenges
	formTokens *antibot.FormTokens
	// submitNonces dedupes double-clicked submits; a nonce is one form
	// render.
	submitNonces *antibot.Seen
	// Per ip|survey buckets: solving the proof-of-work buys a much higher
	// ceiling than skipping it.
	limitChallenged   *antibot.Limiter
	limitUnchallenged *antibot.Limiter
	limitChallengeAPI *antibot.Limiter
}

// NewHandler builds the full request-handling chain for earful serve.
func NewHandler(cfg config.Config, logger *slog.Logger, deps Deps) http.Handler {
	if deps.Clock == nil {
		deps.Clock = clock.Real{}
	}
	if deps.Email == nil {
		deps.Email = email.NewConsole(os.Stdout)
	}
	if deps.AI == nil {
		deps.AI = ai.FromConfig(cfg)
	}
	surveys := store.NewSurveys(deps.Pool)
	authSvc := auth.NewService(deps.Pool, deps.Clock, deps.Email, cfg.BaseURL)
	// While the private beta is on, no path may create an account except
	// invite-code signup — this closes the Google/magic side doors (M12).
	authSvc.SetBetaMode(cfg.BetaMode)
	s := &server{
		cfg:         cfg,
		logger:      logger,
		pool:        deps.Pool,
		clock:       deps.Clock,
		auth:        authSvc,
		surveys:     surveys,
		invites:     invites.NewService(surveys, deps.Email, deps.Clock, cfg.BaseURL),
		emailSender: deps.Email,
		google:      deps.Google,
		ai:          deps.AI,
		aiMeter: &ai.Meter{
			Store:                surveys,
			Clock:                deps.Clock,
			WorkspaceDailyTokens: cfg.AIWorkspaceDailyTokens,
			DailyBudgetEUR:       cfg.AIDailyBudgetEUR,
			CostPer1KTokensEUR:   cfg.AICostPer1KTokensEUR,
			Logger:               logger,
		},

		challenges:        antibot.NewChallenges(deps.Clock),
		formTokens:        antibot.NewFormTokens(deps.Clock),
		submitNonces:      antibot.NewSeen(time.Hour, deps.Clock),
		limitChallenged:   antibot.NewLimiter(30, time.Hour, deps.Clock),
		limitUnchallenged: antibot.NewLimiter(5, time.Hour, deps.Clock),
		limitChallengeAPI: antibot.NewLimiter(120, time.Hour, deps.Clock),
	}

	// Senders with an event feed (Brevo live, Capture in tests) push
	// bounce/complaint events into the suppression list (M4-T4/T6).
	if es, ok := deps.Email.(email.EventSender); ok {
		es.SetEventHandler(func(ctx context.Context, address, reason string) {
			if err := surveys.Suppress(ctx, address, reason, deps.Clock.Now()); err != nil {
				logger.Error("suppress from webhook failed", "error", err)
			}
		})
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Order (outermost first): Recover catches everything, RequestLogger
	// sees every request, CrossOriginProtection rejects cross-origin
	// browser mutations before any handler runs (stdlib Sec-Fetch-Site /
	// Origin check) — the baseline CSRF wall; authenticated mutations
	// additionally require the per-session synchronizer token
	// (requireCSRF). BasicAuthGate (staging only) sits inside
	// SecurityHeaders so its 401 challenges are logged and carry the
	// security headers, and outside the mux so nothing serves ungated.
	var cop http.CrossOriginProtection
	var h http.Handler = cop.Handler(mux)
	h = limitBody(h)
	h = BasicAuthGate(cfg)(h)
	h = SecurityHeaders(cfg)(h)
	h = RequestLogger(logger)(h)
	h = Recover(logger)(h)
	return h
}

// maxRequestBytes caps every request body as defense-in-depth. The largest
// legitimate body is a ~1 MB participant CSV plus multipart overhead, so
// 4 MB is generous while still far below Go's 32 MB multipart default —
// bounding memory pressure from oversized or slow-drip bodies. Individual
// handlers keep their own tighter limits (the CSV read is 1 MB).
const maxRequestBytes = 4 << 20

// limitBody wraps each request body in http.MaxBytesReader so no handler
// can be made to read an unbounded amount. A handler that reads past the
// cap gets an error from the body reader (surfaced as 413 on write).
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		}
		next.ServeHTTP(w, r)
	})
}

// render writes a templ component with an explicit status code.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		// Status and headers are already sent; just surface it in logs.
		slog.Default().Debug("render failed", "error", err)
	}
}
