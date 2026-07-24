package http

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"

	"github.com/TryEarful/earful/internal/config"
)

// BasicAuthGate walls the whole staging deployment behind HTTP Basic Auth
// — staging is a test bench, not a public site, and config refuses to
// boot staging without the credential. Probes stay open: /healthz (Cloud
// Run's startup probe) and /health (the uptime check, which would page
// within minutes behind a 401). Outside staging the gate is a no-op —
// production is public by design.
func BasicAuthGate(cfg config.Config) func(http.Handler) http.Handler {
	user, pass, ok := cfg.BasicAuthCredentials()
	enabled := ok && cfg.Env == config.EnvStaging
	// Comparing sha256 digests keeps the comparison constant-time across
	// lengths too, which ConstantTimeCompare alone does not.
	wantUser := sha256.Sum256([]byte(user))
	wantPass := sha256.Sum256([]byte(pass))
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			gotUser, gotPass, ok := r.BasicAuth()
			gu := sha256.Sum256([]byte(gotUser))
			gp := sha256.Sum256([]byte(gotPass))
			userOK := subtle.ConstantTimeCompare(gu[:], wantUser[:]) == 1
			passOK := subtle.ConstantTimeCompare(gp[:], wantPass[:]) == 1
			if !ok || !userOK || !passOK {
				w.Header().Set("WWW-Authenticate", `Basic realm="earful staging", charset="UTF-8"`)
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
