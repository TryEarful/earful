package http

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/TryEarful/earful/internal/config"
)

// wallCookie carries proof that this browser already passed the staging
// wall, because Basic Auth alone cannot get a WebSocket through it:
// Chrome does not send cached HTTP credentials on a handshake, so a
// browser that is happily loading pages still gets a 401 the moment
// voice or streamed generation opens a socket. Found on the promotion
// gate, then reproduced by hand — it broke staging for people, not only
// for the suite.
//
// Cookies do travel with a same-origin handshake, so one is set the
// first time credentials check out and accepted from then on.
const wallCookie = "earful_staging_wall"

// wallToken is what that cookie holds. It is derived from the credential
// rather than randomly generated per process, because Cloud Run runs
// several instances and a random secret would log a browser back out
// every time it hit a different one. Knowing the token means knowing the
// credential; this is a wall around a test bench, and the application's
// own authentication is what actually protects anything.
func wallToken(user, pass string) string {
	sum := sha256.Sum256([]byte("earful-staging-wall\x00" + user + "\x00" + pass))
	return hex.EncodeToString(sum[:])
}

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
	token := wallToken(user, pass)
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}
			if c, err := r.Cookie(wallCookie); err == nil &&
				subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1 {
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
			http.SetCookie(w, &http.Cookie{
				Name:     wallCookie,
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteLaxMode,
				MaxAge:   int((12 * time.Hour).Seconds()),
			})
			next.ServeHTTP(w, r)
		})
	}
}
