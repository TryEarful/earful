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
	t.Setenv("STAGING_BASIC_AUTH", "user:pass") // staging refuses to boot without it
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

// TestLoad_StagingRequiresBasicAuth: staging is a test bench walled off
// behind HTTP Basic Auth — a boot-time invariant like the console-only
// email sender, so a missing credential can never silently publish it.
func TestLoad_StagingRequiresBasicAuth(t *testing.T) {
	t.Run("missing on staging", func(t *testing.T) {
		t.Setenv("APP_ENV", "staging")
		if _, err := config.Load(); err == nil {
			t.Fatal("Load() accepted APP_ENV=staging without STAGING_BASIC_AUTH")
		}
	})

	for _, malformed := range []string{"nocolon", ":pass", "user:"} {
		t.Run("malformed "+malformed, func(t *testing.T) {
			t.Setenv("APP_ENV", "staging")
			t.Setenv("STAGING_BASIC_AUTH", malformed)
			if _, err := config.Load(); err == nil {
				t.Fatalf("Load() accepted STAGING_BASIC_AUTH=%q", malformed)
			}
		})
	}

	t.Run("optional outside staging", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		if _, err := config.Load(); err != nil {
			t.Fatalf("Load() rejected development without STAGING_BASIC_AUTH: %v", err)
		}
	})
}

func TestBasicAuthCredentials_SplitsAtFirstColon(t *testing.T) {
	user, pass, ok := (config.Config{StagingBasicAuth: "earful:pa:ss"}).BasicAuthCredentials()
	if !ok || user != "earful" || pass != "pa:ss" {
		t.Errorf("BasicAuthCredentials() = %q, %q, %v; want earful, pa:ss, true", user, pass, ok)
	}
	if _, _, ok := (config.Config{}).BasicAuthCredentials(); ok {
		t.Error("BasicAuthCredentials() ok = true for empty value")
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
		t.Setenv("STAGING_BASIC_AUTH", "user:pass")
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

// TestLoad_AIProviders covers the provider whitelist and the conditional
// requirements each backend carries. Model IDs are configuration, so the
// only thing config can insist on is that *some* model is named.
func TestLoad_AIProviders(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr bool
	}{
		{"none by default", nil, false},
		{"openai needs a model", map[string]string{"AI_PROVIDER": "openai"}, true},
		{"openai with a model", map[string]string{"AI_PROVIDER": "openai", "AI_MODEL": "qwen"}, false},
		{"vertex needs a project", map[string]string{"AI_PROVIDER": "vertex", "AI_MODEL": "flash"}, true},
		{"vertex needs a model", map[string]string{
			"AI_PROVIDER": "vertex", "VERTEX_PROJECT": "earful-stg",
		}, true},
		{"vertex with a per-operation model only", map[string]string{
			"AI_PROVIDER": "vertex", "VERTEX_PROJECT": "earful-stg", "AI_MODEL_GENERATE": "flash",
		}, false},
		{"unknown provider", map[string]string{"AI_PROVIDER": "openai-ish"}, true},
		{"unknown transcriber", map[string]string{"TRANSCRIBE_PROVIDER": "deepgram"}, true},
		{"whisper-cli needs a model file", map[string]string{"TRANSCRIBE_PROVIDER": "whisper-cli"}, true},
		{"vertex transcription", map[string]string{
			"TRANSCRIBE_PROVIDER": "vertex", "VERTEX_PROJECT": "earful-stg", "AI_MODEL": "flash",
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := config.Load()
			if tc.wantErr && err == nil {
				t.Errorf("Load() accepted %v", tc.env)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Load() rejected %v: %v", tc.env, err)
			}
		})
	}
}

// TestLoad_ScriptedProviderIsDevelopmentOnly: the scripted provider
// invents content. A deployed environment serving it would be presenting
// canned text as AI output, so it is refused at boot — the same shape of
// invariant as staging's console-only sender.
func TestLoad_ScriptedProviderIsDevelopmentOnly(t *testing.T) {
	t.Run("development", func(t *testing.T) {
		t.Setenv("APP_ENV", "development")
		t.Setenv("AI_PROVIDER", "scripted")
		if _, err := config.Load(); err != nil {
			t.Fatalf("Load() rejected development+scripted: %v", err)
		}
	})
	for _, env := range []string{"staging", "production"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			t.Setenv("STAGING_BASIC_AUTH", "user:pass")
			t.Setenv("AI_PROVIDER", "scripted")
			if _, err := config.Load(); err == nil {
				t.Fatalf("Load() accepted AI_PROVIDER=scripted in %s", env)
			}
			t.Setenv("AI_PROVIDER", "none")
			t.Setenv("TRANSCRIBE_PROVIDER", "scripted")
			if _, err := config.Load(); err == nil {
				t.Fatalf("Load() accepted TRANSCRIBE_PROVIDER=scripted in %s", env)
			}
		})
	}
}
