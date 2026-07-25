package http

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/web/templates"
)

// M12: the private-beta gate — invite-code signup, password login,
// email change, and the super-admin code/reset surface. No handler in
// this file ever sends an email; that is the whole point.

// signupPage exists only while beta mode is on; otherwise the URL is a
// plain 404 (magic-link first-login is the non-beta signup).
func (s *server) signupPage(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.BetaMode {
		http.NotFound(w, r)
		return
	}
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		if _, err := s.auth.Authenticate(r.Context(), c.Value); err == nil {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
	}
	render(w, r, http.StatusOK, templates.Signup(""))
}

func (s *server) signupSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.BetaMode {
		http.NotFound(w, r)
		return
	}
	user, _, err := s.auth.SignupWithCode(r.Context(),
		r.PostFormValue("email"), r.PostFormValue("password"), r.PostFormValue("code"), s.clientIP(r))
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		render(w, r, http.StatusTooManyRequests, templates.ErrorPage(
			"Too many attempts",
			"Sign-up is temporarily paused for this address. Wait a little while and try again."))
	case errors.Is(err, auth.ErrInvalidCode):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Signup("That invite code isn't valid — it may already be used. Check it, or ask for a fresh one."))
	case errors.Is(err, auth.ErrInvalidEmail):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Signup("That doesn't look like an email address — check it and try again."))
	case errors.Is(err, auth.ErrWeakPassword):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Signup("Passwords need at least 8 characters."))
	case errors.Is(err, auth.ErrEmailTaken):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Signup("An account with that email already exists — sign in instead."))
	case err != nil:
		s.logger.Error("beta signup failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "We couldn't create your account. Please try again."))
	default:
		s.startSession(w, r, user.ID)
	}
}

// passwordLogin authenticates email+password. Not beta-gated on purpose:
// accounts that hold a password keep working the day beta mode turns
// off. Every failure is the same sentence.
func (s *server) passwordLogin(w http.ResponseWriter, r *http.Request) {
	user, _, err := s.auth.LoginWithPassword(r.Context(),
		r.PostFormValue("email"), r.PostFormValue("password"), s.clientIP(r))
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		render(w, r, http.StatusTooManyRequests, templates.ErrorPage(
			"Too many attempts",
			"Sign-in is temporarily paused for this address. Wait a little while and try again."))
	case errors.Is(err, auth.ErrBadCredentials):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Login(s.google != nil, s.cfg.BetaMode, "Invalid email or password.", ""))
	case err != nil:
		s.logger.Error("password login failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "We couldn't sign you in. Please try again."))
	default:
		s.startSession(w, r, user.ID)
	}
}

// accountEmail applies an email change immediately after re-proving the
// current password (verification-by-email upgrades this when an ESP
// exists).
func (s *server) accountEmail(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	err := s.auth.ChangeEmail(r.Context(), info.UserID,
		r.PostFormValue("email"), r.PostFormValue("password"))
	rerender := func(msg string) {
		render(w, r, http.StatusUnprocessableEntity,
			templates.Account(info.Email, info.WorkspaceName, info.CSRFToken,
				templates.AccountData{IsSuperAdmin: info.IsSuperAdmin, EmailError: msg}))
	}
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		rerender("That doesn't look like an email address — check it and try again.")
	case errors.Is(err, auth.ErrBadCredentials):
		rerender("That password isn't right. Your email is unchanged.")
	case errors.Is(err, auth.ErrEmailTaken):
		rerender("Another account already uses that address.")
	case errors.Is(err, auth.ErrNoPassword):
		rerender("This account signs in with Google and has no password. Ask an admin to set one first.")
	case err != nil:
		s.logger.Error("email change failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "We couldn't change your email. Please try again."))
	default:
		http.Redirect(w, r, "/account?notice=email_changed", http.StatusSeeOther)
	}
}

// --- super-admin surface ---

func (s *server) adminBetaCodesPage(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	rows, err := s.betaCodeRows(r)
	if err != nil {
		s.logger.Error("list beta codes failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "Couldn't load the code list."))
		return
	}
	render(w, r, http.StatusOK,
		templates.BetaCodesAdmin(info.Email, info.CSRFToken, nil, "", "", rows, ""))
}

func (s *server) adminBetaCodesMint(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	count, err := strconv.Atoi(r.PostFormValue("count"))
	if err != nil || count < 1 {
		count = 1
	}
	if count > 50 {
		count = 50
	}
	minted, err := s.auth.MintBetaCodes(r.Context(), count, r.PostFormValue("label"))
	if err != nil {
		s.logger.Error("mint beta codes failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "Couldn't mint codes. Please try again."))
		return
	}
	rows, _ := s.betaCodeRows(r)
	render(w, r, http.StatusOK,
		templates.BetaCodesAdmin(info.Email, info.CSRFToken, minted, "", "", rows, ""))
}

func (s *server) adminBetaCodesRevoke(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PostFormValue("id"))
	if err == nil {
		err = s.auth.RevokeBetaCode(r.Context(), id)
	}
	if err != nil && !errors.Is(err, auth.ErrInvalidCode) {
		s.logger.Error("revoke beta code failed", "error", err)
	}
	// Used/unknown ids fall through silently — the refreshed list is the
	// truth either way.
	http.Redirect(w, r, "/admin/beta-codes", http.StatusSeeOther)
}

func (s *server) adminResetPassword(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	address := r.PostFormValue("email")
	temp, err := s.auth.AdminResetPassword(r.Context(), address)
	rows, _ := s.betaCodeRows(r)
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		render(w, r, http.StatusUnprocessableEntity,
			templates.BetaCodesAdmin(info.Email, info.CSRFToken, nil, "", "", rows, "No account with that email."))
	case err != nil:
		s.logger.Error("admin password reset failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			"Something went wrong", "Couldn't reset the password. Please try again."))
	default:
		render(w, r, http.StatusOK,
			templates.BetaCodesAdmin(info.Email, info.CSRFToken, nil, address, temp, rows, ""))
	}
}

func (s *server) betaCodeRows(r *http.Request) ([]templates.BetaCodeRow, error) {
	list, err := s.auth.ListBetaCodes(r.Context())
	if err != nil {
		return nil, err
	}
	rows := make([]templates.BetaCodeRow, 0, len(list))
	for _, c := range list {
		status := "unused"
		switch {
		case c.RevokedAt != nil:
			status = "revoked"
		case c.UsedAt != nil:
			status = "used"
			if c.UsedByEmail != nil {
				status = "used by " + *c.UsedByEmail
			}
		}
		rows = append(rows, templates.BetaCodeRow{
			ID:      c.ID.String(),
			Label:   c.Label,
			Created: c.CreatedAt.Format("2006-01-02"),
			Status:  status,
		})
	}
	return rows, nil
}
