package http

import (
	"errors"
	"github.com/TryEarful/earful/internal/uitext"
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
			say(r, "error.attempts.title"),
			say(r, "signup.error.paused")))
	case errors.Is(err, auth.ErrInvalidCode):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Signup(say(r, "signup.error.code")))
	case errors.Is(err, auth.ErrInvalidEmail):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Signup(say(r, "auth.error.email")))
	case errors.Is(err, auth.ErrWeakPassword):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Signup(say(r, "signup.error.password")))
	case errors.Is(err, auth.ErrEmailTaken):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Signup(say(r, "signup.error.exists")))
	case err != nil:
		s.logger.Error("beta signup failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			say(r, "error.generic.title"), say(r, "signup.error.failed")))
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
			say(r, "error.attempts.title"),
			say(r, "login.error.paused")))
	case errors.Is(err, auth.ErrBadCredentials):
		render(w, r, http.StatusUnprocessableEntity,
			templates.Login(s.google != nil, s.cfg.BetaMode, say(r, "login.error.credentials"), ""))
	case err != nil:
		s.logger.Error("password login failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			say(r, "error.generic.title"), say(r, "login.error.failed")))
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
				templates.AccountData{IsSuperAdmin: info.IsSuperAdmin, HasPassword: true, EmailError: msg}))
	}
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		rerender(say(r, "auth.error.email"))
	case errors.Is(err, auth.ErrBadCredentials):
		rerender(say(r, "account.error.email.password"))
	case errors.Is(err, auth.ErrEmailTaken):
		rerender(say(r, "account.error.email.taken"))
	case errors.Is(err, auth.ErrNoPassword):
		rerender(say(r, "account.error.email.google"))
	case err != nil:
		s.logger.Error("email change failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			say(r, "error.generic.title"), say(r, "account.error.email.failed")))
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
			say(r, "error.generic.title"), say(r, "admin.beta.error.list")))
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
			say(r, "error.generic.title"), say(r, "admin.beta.error.mint")))
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
			templates.BetaCodesAdmin(info.Email, info.CSRFToken, nil, "", "", rows, say(r, "admin.beta.error.account")))
	case err != nil:
		s.logger.Error("admin password reset failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			say(r, "error.generic.title"), say(r, "admin.beta.error.reset")))
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
		status := say(r, "admin.beta.state.unused")
		switch {
		case c.RevokedAt != nil:
			status = say(r, "admin.beta.state.revoked")
		case c.UsedAt != nil:
			status = say(r, "admin.beta.state.used")
			if c.UsedByEmail != nil {
				status = say(r, "admin.beta.state.used_by", uitext.Args{"Email": *c.UsedByEmail})
			}
		}
		rows = append(rows, templates.BetaCodeRow{
			ID:        c.ID.String(),
			Label:     c.Label,
			Created:   c.CreatedAt.Format("2006-01-02"),
			Status:    status,
			Revocable: c.RevokedAt == nil && c.UsedAt == nil,
		})
	}
	return rows, nil
}
