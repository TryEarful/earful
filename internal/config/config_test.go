package config_test

import (
	"log/slog"
	"testing"

	"github.com/TryEarful/earful/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != config.EnvDevelopment {
		t.Errorf("Env = %q, want %q", cfg.Env, config.EnvDevelopment)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.DatabaseURL != "" {
		t.Errorf("DatabaseURL = %q, want empty", cfg.DatabaseURL)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want Info", cfg.LogLevel)
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://localhost/earful")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != "staging" {
		t.Errorf("Env = %q, want staging", cfg.Env)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://localhost/earful" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, want Debug", cfg.LogLevel)
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	t.Setenv("PORT", "not-a-number")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() error = nil, want error for non-numeric PORT")
	}
}

func TestLoad_PortOutOfRange(t *testing.T) {
	t.Setenv("PORT", "70000")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() error = nil, want error for out-of-range PORT")
	}
}

func TestLoad_InvalidEnv(t *testing.T) {
	t.Setenv("APP_ENV", "bogus")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() error = nil, want error for invalid APP_ENV")
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "bogus")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() error = nil, want error for invalid LOG_LEVEL")
	}
}

func TestLoad_M2Defaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BaseURL != "http://localhost:8080" {
		t.Errorf("BaseURL = %q, want http://localhost:8080", cfg.BaseURL)
	}
	if cfg.EmailSender != "console" {
		t.Errorf("EmailSender = %q, want console", cfg.EmailSender)
	}
	if cfg.GoogleLoginEnabled() {
		t.Error("GoogleLoginEnabled() = true with no credentials configured")
	}
}

func TestLoad_TrimsBaseURLTrailingSlash(t *testing.T) {
	t.Setenv("BASE_URL", "https://stg.tryearful.com/")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BaseURL != "https://stg.tryearful.com" {
		t.Errorf("BaseURL = %q, want no trailing slash", cfg.BaseURL)
	}
}

func TestLoad_InvalidBaseURL(t *testing.T) {
	t.Setenv("BASE_URL", "not-a-url")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() error = nil, want error for non-absolute BASE_URL")
	}
}

func TestLoad_UnknownEmailSender(t *testing.T) {
	t.Setenv("EMAIL_SENDER", "brevo")
	if _, err := config.Load(); err == nil {
		t.Fatal("Load() error = nil, want error for an unimplemented EMAIL_SENDER")
	}
}

func TestGoogleLoginEnabled_NeedsBothCredentials(t *testing.T) {
	if (config.Config{GoogleClientID: "id"}).GoogleLoginEnabled() {
		t.Error("client ID alone should not enable Google login")
	}
	if (config.Config{GoogleClientSecret: "secret"}).GoogleLoginEnabled() {
		t.Error("client secret alone should not enable Google login")
	}
	if !(config.Config{GoogleClientID: "id", GoogleClientSecret: "secret"}).GoogleLoginEnabled() {
		t.Error("both credentials should enable Google login")
	}
}

func TestSecureCookies_OnlyDevelopmentIsExempt(t *testing.T) {
	if (config.Config{Env: config.EnvDevelopment}).SecureCookies() {
		t.Error("development should not require Secure cookies (plain http localhost)")
	}
	for _, env := range []string{config.EnvStaging, config.EnvProduction} {
		if !(config.Config{Env: env}).SecureCookies() {
			t.Errorf("%s must require Secure cookies", env)
		}
	}
}

func TestRequireDatabaseURL(t *testing.T) {
	empty := config.Config{}
	if err := empty.RequireDatabaseURL(); err == nil {
		t.Error("RequireDatabaseURL() error = nil, want error for empty DatabaseURL")
	}

	set := config.Config{DatabaseURL: "postgres://localhost/earful"}
	if err := set.RequireDatabaseURL(); err != nil {
		t.Errorf("RequireDatabaseURL() error = %v, want nil", err)
	}
}

// TestLoad_StagingNeverSendsEmail: staging's console sender is a hard
// boot-time invariant (the smoke gate depends on it, and staging must
// never spray test email at real inboxes) — not a convention.
func TestLoad_StagingNeverSendsEmail(t *testing.T) {
	for _, sender := range []string{"smtp", "brevo"} {
		t.Run(sender, func(t *testing.T) {
			t.Setenv("APP_ENV", "staging")
			t.Setenv("EMAIL_SENDER", sender)
			t.Setenv("BREVO_API_KEY", "irrelevant")
			if _, err := config.Load(); err == nil {
				t.Fatalf("Load() accepted APP_ENV=staging with EMAIL_SENDER=%s; staging must be console-only", sender)
			}
		})
	}

	t.Run("console allowed", func(t *testing.T) {
		t.Setenv("APP_ENV", "staging")
		t.Setenv("EMAIL_SENDER", "console")
		if _, err := config.Load(); err != nil {
			t.Fatalf("Load() rejected staging+console: %v", err)
		}
	})

	t.Run("smtp stays legal outside staging", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("EMAIL_SENDER", "smtp")
		if _, err := config.Load(); err != nil {
			t.Fatalf("Load() rejected development+smtp (mailpit): %v", err)
		}
	})
}
