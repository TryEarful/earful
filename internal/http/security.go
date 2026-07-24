package http

import (
	"net/http"
	"strings"

	"github.com/TryEarful/earful/internal/config"
)

// contentSecurityPolicy is deliberately first-party only, with no inline
// script or style anywhere (ADR-0006, M4-T7). Respondent pages must load
// nothing from a third party — that is the promise — and applying the same
// policy to creator pages means a violation shows up in development
// rather than only in production.
//
// Consequences accepted on purpose:
//   - every script lives in web/static/js, none inline; the respondent
//     enhancement script is written against this constraint
//   - frame-ancestors 'none' blocks embedding surveys in other sites,
//     which PLAN.md lists as out of scope pending a CSP/anti-bot redesign
//   - form-action 'self' means a stolen page cannot post answers elsewhere
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"form-action 'self'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'; " +
	"object-src 'none'"

// SecurityHeaders applies the response headers every page needs.
func SecurityHeaders(cfg config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			// Tokened URLs are personal data (participant invite links,
			// magic links). no-referrer keeps them out of the Referer
			// header on any outbound navigation.
			h.Set("Referrer-Policy", "no-referrer")
			// Redundant with frame-ancestors for modern browsers, kept for
			// older ones.
			h.Set("X-Frame-Options", "DENY")
			// Respondent pages need no device APIs at all except the
			// microphone, which M5 will enable for voice answering.
			h.Set("Permissions-Policy", "camera=(), geolocation=(), payment=(), usb=()")

			// HSTS only where TLS actually terminates: sending it over
			// plain-http local development would pin localhost to HTTPS in
			// the developer's browser and break the dev server.
			if cfg.Env != config.EnvDevelopment {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			// Dynamic pages must not be cached: without an explicit
			// Cache-Control, browsers heuristically cache responses — a
			// respondent revisiting a reopened survey can be shown the
			// cached "closed" page, and worse, dashboards and answers can
			// end up in disk cache on shared machines. Static assets are
			// safe to cache for a day because templates reference them
			// through static.AssetURL, whose ?v= content fingerprint
			// changes whenever the assets do — a deploy busts the cache by
			// changing the URL, not by waiting out max-age.
			if strings.HasPrefix(r.URL.Path, "/static/") {
				h.Set("Cache-Control", "public, max-age=86400")
			} else {
				h.Set("Cache-Control", "no-store")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// robotsTxt keeps respondent pages out of search indexes and the crawlers
// that feed model training, complementing the per-page noindex meta tag.
// Creator pages are behind authentication and need no rule.
func robotsTxt(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("User-agent: *\nDisallow: /s/\nDisallow: /surveys/\nDisallow: /auth/\n")) //nolint:errcheck
}
