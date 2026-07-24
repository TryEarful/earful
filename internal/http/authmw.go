package http

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/web/templates"
)

const (
	sessionCookieName = "earful_session"
	oauthStateCookie  = "earful_oauth_state"
	oauthNonceCookie  = "earful_oauth_nonce"
)

// setSessionCookie installs the raw session token as the browser session
// credential: HttpOnly always, SameSite=Lax always, Secure outside local
// development (PLAN.md M2-T1).
func (s *server) setSessionCookie(w http.ResponseWriter, raw string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    raw,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

// setEphemeralCookie is for the short-lived OAuth state/nonce pair.
// SameSite=Lax deliberately: the provider redirect back to our callback
// is a cross-site top-level GET navigation, which Lax permits.
func (s *server) setEphemeralCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/auth/google",
		MaxAge:   int((10 * time.Minute).Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) clearEphemeralCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/auth/google",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

// requireAuth resolves the session cookie to an identity and attaches it
// to the request context; unauthenticated requests are redirected to
// /login. Every workspace-scoped handler sits behind this.
func (s *server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		info, err := s.auth.Authenticate(r.Context(), c.Value)
		if err != nil {
			if !errors.Is(err, auth.ErrNoSession) {
				s.logger.Error("authenticate failed", "error", err)
			}
			s.clearSessionCookie(w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(withAuth(r.Context(), info)))
	})
}

// requireSuperAdmin sits inside requireAuth and answers 404 — not 403 —
// to anyone without the instance-level flag: the admin surface should be
// indistinguishable from a URL that doesn't exist (M12).
func (s *server) requireSuperAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := authFrom(r.Context())
		if !ok || !info.IsSuperAdmin {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireCSRF enforces the per-session synchronizer token on
// authenticated mutations (PLAN.md M2-T1: CSRF tokens on all mutations).
// The token arrives as the _csrf form field (or X-CSRF-Token header for
// future JS calls) and must match the session's token exactly.
func (s *server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := authFrom(r.Context())
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		token := r.PostFormValue("_csrf")
		if token == "" {
			token = r.Header.Get("X-CSRF-Token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(info.CSRFToken)) != 1 {
			render(w, r, http.StatusForbidden, templates.ErrorPage(
				"Request blocked",
				"This form was missing its security token. Go back, reload the page, and try again."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the address rate limits key on. In development (and in
// tests that don't say otherwise) it is the socket peer. In deployed
// environments the request arrives through exactly ONE trusted hop —
// Cloud Run's Google Front End, which appends the real client to
// X-Forwarded-For — so the rightmost entry is the trustworthy one and
// anything the client itself put further left is ignored. Only a
// well-formed IP is trusted: a garbage rightmost value falls through to
// the socket peer rather than becoming a spoofed rate-limit key.
//
// This assumes a single trusted proxy. If a load balancer (a global LB,
// Cloud Armor) is ever placed in front of Cloud Run, the real client
// moves one position left and this must count trusted hops from the right
// instead of always taking the last.
func (s *server) clientIP(r *http.Request) string {
	if s.cfg.Env != "development" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			candidate := strings.TrimSpace(parts[len(parts)-1])
			if net.ParseIP(candidate) != nil {
				return candidate
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
