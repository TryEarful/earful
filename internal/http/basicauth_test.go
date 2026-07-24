package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TryEarful/earful/internal/config"
	apphttp "github.com/TryEarful/earful/internal/http"
)

// gated wraps a stub 200 handler in BasicAuthGate — no server, no DB;
// the gate is pure request filtering.
func gated(cfg config.Config) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return apphttp.BasicAuthGate(cfg)(ok)
}

func TestBasicAuthGate_Staging(t *testing.T) {
	cfg := config.Config{Env: config.EnvStaging, StagingBasicAuth: "earful:s3cret"}

	tests := []struct {
		name       string
		path       string
		user, pass string
		setCreds   bool
		want       int
	}{
		{name: "no credentials", path: "/", want: http.StatusUnauthorized},
		{name: "wrong password", path: "/", setCreds: true, user: "earful", pass: "wrong", want: http.StatusUnauthorized},
		{name: "wrong user", path: "/", setCreds: true, user: "nobody", pass: "s3cret", want: http.StatusUnauthorized},
		{name: "correct credentials", path: "/", setCreds: true, user: "earful", pass: "s3cret", want: http.StatusOK},
		{name: "login page gated too", path: "/login", want: http.StatusUnauthorized},
		{name: "healthz open for the startup probe", path: "/healthz", want: http.StatusOK},
		{name: "health open for the uptime check", path: "/health", want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.setCreds {
				r.SetBasicAuth(tt.user, tt.pass)
			}
			w := httptest.NewRecorder()
			gated(cfg).ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
			if tt.want == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") == "" {
				t.Error("401 without a WWW-Authenticate challenge")
			}
		})
	}
}

func TestBasicAuthGate_PasswordMayContainColons(t *testing.T) {
	cfg := config.Config{Env: config.EnvStaging, StagingBasicAuth: "earful:pa:ss"}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.SetBasicAuth("earful", "pa:ss")
	w := httptest.NewRecorder()
	gated(cfg).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a password containing a colon", w.Code)
	}
}

func TestBasicAuthGate_DisabledOutsideStaging(t *testing.T) {
	for _, env := range []string{config.EnvDevelopment, config.EnvProduction} {
		t.Run(env, func(t *testing.T) {
			cfg := config.Config{Env: env, StagingBasicAuth: "earful:s3cret"}
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			gated(cfg).ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 — the gate must be a no-op in %s", w.Code, env)
			}
		})
	}
}

// An empty credential disables the gate rather than locking everyone out;
// config validation — not the middleware — enforces its presence on
// staging.
func TestBasicAuthGate_EmptyCredentialIsPassthrough(t *testing.T) {
	cfg := config.Config{Env: config.EnvStaging}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	gated(cfg).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 passthrough for empty StagingBasicAuth", w.Code)
	}
}
