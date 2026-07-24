package http

import (
	"crypto/subtle"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/web/templates"
)

// notices are fixed strings keyed by ?notice= codes so redirects can
// carry a message without reflecting arbitrary query text into the page.
var notices = map[string]string{
	"signed_out":    "You've been signed out.",
	"google_failed": "Google sign-in didn't complete. Try again, or use your email instead.",
}

func (s *server) loginPage(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if _, err := s.auth.Authenticate(r.Context(), c.Value); err == nil {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
	}
	render(w, r, http.StatusOK, templates.Login(s.google != nil, s.cfg.BetaMode, "", notices[r.URL.Query().Get("notice")]))
}

func (s *server) magicRequest(w http.ResponseWriter, r *http.Request) {
	// Beta mode has no email to send — the endpoint plays dead alongside
	// its hidden form (M12).
	if s.cfg.BetaMode {
		http.NotFound(w, r)
		return
	}
	address := r.PostFormValue("email")
	err := s.auth.RequestMagicLink(r.Context(), address, s.clientIP(r))
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Login(s.google != nil, false, "That doesn't look like an email address — check it and try again.", ""))
	case errors.Is(err, auth.ErrRateLimited):
		render(w, r, http.StatusTooManyRequests, templates.ErrorPage(
			"Too many sign-in requests",
			"We've sent several links recently. Wait a little while, check your inbox for an earlier link, then try again."))
	case err != nil:
		s.logger.Error("magic link request failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "We couldn't send your sign-in link. Please try again."))
	default:
		render(w, r, http.StatusOK, templates.MagicSent(address))
	}
}

// magicVerifyPage (GET) only *peeks* at the token and renders a
// confirmation form; consumption happens exclusively on POST so that
// email-security scanners prefetching the link cannot burn it.
func (s *server) magicVerifyPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		render(w, r, http.StatusBadRequest, templates.MagicInvalid("The sign-in link is incomplete. Use the full link from your email."))
		return
	}
	address, err := s.auth.PeekMagicToken(r.Context(), token)
	if err != nil {
		render(w, r, http.StatusBadRequest, templates.MagicInvalid(magicErrorMessage(err)))
		return
	}
	render(w, r, http.StatusOK, templates.MagicConfirm(address, token))
}

func (s *server) magicVerifyConsume(w http.ResponseWriter, r *http.Request) {
	token := r.PostFormValue("token")
	address, err := s.auth.ConsumeMagicToken(r.Context(), token)
	if err != nil {
		render(w, r, http.StatusBadRequest, templates.MagicInvalid(magicErrorMessage(err)))
		return
	}
	user, _, err := s.auth.LoginByEmail(r.Context(), address)
	if err != nil {
		s.logger.Error("magic login failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "We couldn't complete your sign-in. Please request a fresh link."))
		return
	}
	s.startSession(w, r, user.ID)
}

func magicErrorMessage(err error) string {
	switch {
	case errors.Is(err, auth.ErrExpiredToken):
		return "This sign-in link has expired (links last 15 minutes). Request a fresh one."
	case errors.Is(err, auth.ErrUsedToken):
		return "This sign-in link was already used. Request a fresh one to sign in again."
	default:
		return "This sign-in link isn't valid. Request a fresh one."
	}
}

func (s *server) googleStart(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		http.NotFound(w, r)
		return
	}
	state, nonce := auth.NewToken(), auth.NewToken()
	s.setEphemeralCookie(w, oauthStateCookie, state)
	s.setEphemeralCookie(w, oauthNonceCookie, nonce)
	http.Redirect(w, r, s.google.AuthCodeURL(state, nonce), http.StatusFound)
}

func (s *server) googleCallback(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		http.NotFound(w, r)
		return
	}
	defer func() {
		s.clearEphemeralCookie(w, oauthStateCookie)
		s.clearEphemeralCookie(w, oauthNonceCookie)
	}()

	if r.URL.Query().Get("error") != "" {
		// User cancelled or provider refused; no details worth showing.
		http.Redirect(w, r, "/login?notice=google_failed", http.StatusSeeOther)
		return
	}
	stateCookie, err := r.Cookie(oauthStateCookie)
	if err != nil || stateCookie.Value == "" ||
		subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(r.URL.Query().Get("state"))) != 1 {
		render(w, r, http.StatusForbidden, templates.ErrorPage(
			"Sign-in blocked", "The sign-in attempt could not be verified (state mismatch). Start again from the login page."))
		return
	}
	nonceCookie, err := r.Cookie(oauthNonceCookie)
	if err != nil || nonceCookie.Value == "" {
		render(w, r, http.StatusForbidden, templates.ErrorPage(
			"Sign-in blocked", "The sign-in attempt could not be verified. Start again from the login page."))
		return
	}

	identity, err := s.google.Exchange(r.Context(), r.URL.Query().Get("code"), nonceCookie.Value)
	if err != nil {
		s.logger.Warn("google exchange failed", "error", err)
		http.Redirect(w, r, "/login?notice=google_failed", http.StatusSeeOther)
		return
	}
	user, _, err := s.auth.LoginByGoogle(r.Context(), identity.Sub, identity.Email)
	if err != nil {
		s.logger.Error("google login failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "We couldn't complete your sign-in. Please try again."))
		return
	}
	s.startSession(w, r, user.ID)
}

// startSession issues a brand-new session cookie and lands the user on
// the dashboard. Always a fresh token — a pre-login cookie value is never
// kept, which is what forecloses session fixation.
func (s *server) startSession(w http.ResponseWriter, r *http.Request, userID uuid.UUID) {
	raw, sess, err := s.auth.CreateSession(r.Context(), userID)
	if err != nil {
		s.logger.Error("create session failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "We couldn't complete your sign-in. Please try again."))
		return
	}
	s.setSessionCookie(w, raw, sess.ExpiresAt)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if err := s.auth.Logout(r.Context(), c.Value); err != nil {
			s.logger.Error("logout failed", "error", err)
		}
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login?notice=signed_out", http.StatusSeeOther)
}
