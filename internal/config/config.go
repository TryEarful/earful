// Package config provides the single, 12-factor env-based configuration
// surface for the earful binary. Every subcommand and every future
// milestone's settings (session secrets, AI provider keys, email
// credentials, ...) are added as fields here — never read ad hoc from
// os.Getenv elsewhere.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds all runtime configuration, sourced from environment
// variables. Zero value is not valid; use Load.
type Config struct {
	// Env is the deployment environment: development, staging, or production.
	Env string
	// Port is the TCP port the HTTP server listens on.
	Port int
	// DatabaseURL is the Postgres connection string. May be empty for
	// commands that don't need a database (e.g. purge --dry-run);
	// callers that require it must call RequireDatabaseURL.
	DatabaseURL string
	// LogLevel controls the minimum slog level emitted.
	LogLevel slog.Level
	// BaseURL is the externally-visible origin of this deployment
	// (no trailing slash), used to build absolute links in emails.
	BaseURL string
	// EmailSender selects the outbound email implementation: "console"
	// (dev default, prints to stdout), "smtp" (mailpit locally,
	// self-hosters' relays in production), or "brevo" (the SaaS ESP,
	// ADR-0005).
	EmailSender string
	// EmailFrom is the From address for smtp and brevo senders.
	EmailFrom string
	// SMTPAddr is host:port of the relay; SMTPUser/SMTPPass are optional
	// (mailpit needs none).
	SMTPAddr string
	SMTPUser string
	SMTPPass string
	// BrevoAPIKey authenticates against the Brevo API.
	BrevoAPIKey string
	// EmailWebhookSecret is the shared secret in the ESP webhook URL
	// (/webhooks/email/{secret}); empty disables the endpoint.
	EmailWebhookSecret string

	// --- AI (M6). Model IDs are configuration, never code. ---

	// AIProvider selects the text-AI backend: "none" (features degrade
	// gracefully, Appendix D) or "openai" (any OpenAI-compatible server:
	// ollama's /v1, llamafile, ...). "vertex" arrives with the cloud
	// milestone.
	AIProvider string
	AIBaseURL  string
	AIModel    string
	AIAPIKey   string
	// TranscribeProvider selects the voice backend: "none",
	// "whisper-cli" (local whisper.cpp binary), or "openai" (the text
	// backend's whisper-style audio endpoint).
	TranscribeProvider string
	WhisperBin         string
	WhisperModel       string
	// AIDailyBudgetEUR is the global daily breaker (story 67);
	// AIWorkspaceDailyTokens the per-workspace cap (story 21);
	// AICostPer1KTokensEUR converts token estimates to cost estimates.
	AIDailyBudgetEUR       float64
	AIWorkspaceDailyTokens int64
	AICostPer1KTokensEUR   float64
	// GoogleClientID/GoogleClientSecret enable Google OIDC login when
	// both are set; the login page hides the Google option otherwise.
	GoogleClientID     string
	GoogleClientSecret string
	// GoogleIssuer is the OIDC issuer URL. Only tests override it (to a
	// fake issuer); real deployments keep the default.
	GoogleIssuer string
	// BetaMode gates the private beta (M12): signup requires a one-shot
	// invite code, login is email+password, and no account-creating path
	// works without a code — all with zero emails sent. Retires at launch.
	BetaMode bool
	// StagingBasicAuth ("user:pass") walls the whole deployment behind an
	// HTTP Basic Auth challenge — staging is a test bench, not a public
	// site. Required on staging and only active there; /healthz and
	// /health stay open for probes (see BasicAuthGate).
	StagingBasicAuth string
}

// GoogleLoginEnabled reports whether Google OIDC is configured.
func (c Config) GoogleLoginEnabled() bool {
	return c.GoogleClientID != "" && c.GoogleClientSecret != ""
}

// SecureCookies reports whether cookies must carry the Secure attribute.
// Only local development over plain http is exempt.
func (c Config) SecureCookies() bool {
	return c.Env != EnvDevelopment
}

// BasicAuthCredentials splits StagingBasicAuth at its first colon, so
// passwords containing colons survive. ok is false when unset or when
// either part is empty.
func (c Config) BasicAuthCredentials() (user, pass string, ok bool) {
	user, pass, ok = strings.Cut(c.StagingBasicAuth, ":")
	if !ok || user == "" || pass == "" {
		return "", "", false
	}
	return user, pass, true
}

const (
	EnvDevelopment = "development"
	EnvStaging     = "staging"
	EnvProduction  = "production"
)

// Load reads configuration from the environment and validates it.
func Load() (Config, error) {
	cfg := Config{
		Env:                getEnv("APP_ENV", EnvDevelopment),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		BaseURL:            strings.TrimSuffix(getEnv("BASE_URL", "http://localhost:8080"), "/"),
		EmailSender:        getEnv("EMAIL_SENDER", "console"),
		EmailFrom:          getEnv("EMAIL_FROM", "earful@localhost"),
		SMTPAddr:           getEnv("SMTP_ADDR", "localhost:1025"),
		SMTPUser:           getEnv("SMTP_USER", ""),
		SMTPPass:           getEnv("SMTP_PASS", ""),
		BrevoAPIKey:        getEnv("BREVO_API_KEY", ""),
		EmailWebhookSecret: getEnv("EMAIL_WEBHOOK_SECRET", ""),
		AIProvider:         getEnv("AI_PROVIDER", "none"),
		AIBaseURL:          getEnv("AI_BASE_URL", "http://localhost:11434/v1"),
		AIModel:            getEnv("AI_MODEL", ""),
		AIAPIKey:           getEnv("AI_API_KEY", ""),
		TranscribeProvider: getEnv("TRANSCRIBE_PROVIDER", "none"),
		WhisperBin:         getEnv("WHISPER_BIN", "whisper-cli"),
		WhisperModel:       getEnv("WHISPER_MODEL", ""),
		GoogleClientID:     getEnv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getEnv("GOOGLE_CLIENT_SECRET", ""),
		GoogleIssuer:       getEnv("GOOGLE_OIDC_ISSUER", "https://accounts.google.com"),
		BetaMode:           getEnv("BETA_MODE", "false") == "true",
		StagingBasicAuth:   getEnv("STAGING_BASIC_AUTH", ""),
	}

	port, err := strconv.Atoi(getEnv("PORT", "8080"))
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid PORT: %w", err)
	}
	cfg.Port = port

	level, err := parseLogLevel(getEnv("LOG_LEVEL", "info"))
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid LOG_LEVEL: %w", err)
	}
	cfg.LogLevel = level

	cfg.AIDailyBudgetEUR, err = strconv.ParseFloat(getEnv("AI_DAILY_BUDGET_EUR", "3"), 64)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid AI_DAILY_BUDGET_EUR: %w", err)
	}
	cfg.AIWorkspaceDailyTokens, err = strconv.ParseInt(getEnv("AI_WORKSPACE_DAILY_TOKENS", "200000"), 10, 64)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid AI_WORKSPACE_DAILY_TOKENS: %w", err)
	}
	cfg.AICostPer1KTokensEUR, err = strconv.ParseFloat(getEnv("AI_COST_PER_1K_TOKENS_EUR", "0.001"), 64)
	if err != nil {
		return Config{}, fmt.Errorf("config: invalid AI_COST_PER_1K_TOKENS_EUR: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	switch c.Env {
	case EnvDevelopment, EnvStaging, EnvProduction:
	default:
		return fmt.Errorf("config: invalid APP_ENV %q (want development, staging, or production)", c.Env)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("config: PORT %d out of range 1-65535", c.Port)
	}
	if u, err := url.Parse(c.BaseURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("config: BASE_URL %q is not an absolute URL", c.BaseURL)
	}
	switch c.EmailSender {
	case "console", "smtp":
	case "brevo":
		if c.BrevoAPIKey == "" {
			return fmt.Errorf("config: EMAIL_SENDER=brevo requires BREVO_API_KEY")
		}
	default:
		return fmt.Errorf("config: unknown EMAIL_SENDER %q (want console, smtp, or brevo)", c.EmailSender)
	}
	// Staging must never send real email — its console sender is
	// load-bearing: the deploy smoke gate reads magic links back out of
	// Cloud Logging, and a misconfigured staging pointed at an ESP would
	// spray test traffic at real inboxes. Boot-time invariant, not
	// convention.
	if c.Env == EnvStaging && c.EmailSender != "console" {
		return fmt.Errorf("config: APP_ENV=staging must use EMAIL_SENDER=console (staging never sends real email; got %q)", c.EmailSender)
	}
	// Staging is a test bench, not a public site: every route except the
	// probes sits behind HTTP Basic Auth (BasicAuthGate). Enforced at
	// boot so a missing credential can never silently publish staging.
	if c.Env == EnvStaging && c.StagingBasicAuth == "" {
		return fmt.Errorf("config: APP_ENV=staging requires STAGING_BASIC_AUTH (user:pass)")
	}
	if _, _, ok := c.BasicAuthCredentials(); c.StagingBasicAuth != "" && !ok {
		return fmt.Errorf("config: STAGING_BASIC_AUTH must be user:pass with both parts non-empty")
	}
	switch c.AIProvider {
	case "none":
	case "openai":
		if c.AIModel == "" {
			return fmt.Errorf("config: AI_PROVIDER=openai requires AI_MODEL")
		}
	default:
		return fmt.Errorf("config: unknown AI_PROVIDER %q (want none or openai; vertex arrives with the cloud milestone)", c.AIProvider)
	}
	switch c.TranscribeProvider {
	case "none", "openai":
	case "whisper-cli":
		if c.WhisperModel == "" {
			return fmt.Errorf("config: TRANSCRIBE_PROVIDER=whisper-cli requires WHISPER_MODEL")
		}
	default:
		return fmt.Errorf("config: unknown TRANSCRIBE_PROVIDER %q (want none, whisper-cli, or openai)", c.TranscribeProvider)
	}
	return nil
}

// RequireDatabaseURL returns an error if DatabaseURL is unset. Called by
// subcommands (migrate, purge) that cannot proceed without a database;
// serve does not require it in M0.
func (c Config) RequireDatabaseURL() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("config: DATABASE_URL is required for this command")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown level %q (want debug, info, warn, or error)", s)
	}
}
