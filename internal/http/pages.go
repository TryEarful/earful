package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

func (s *server) accountPage(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	data := templates.AccountData{IsSuperAdmin: info.IsSuperAdmin}
	hasPassword, err := s.auth.HasPassword(r.Context(), info.UserID)
	if err != nil {
		s.internalError(w, r, "read account", err)
		return
	}
	data.HasPassword = hasPassword
	switch r.URL.Query().Get("notice") {
	case "email_changed":
		data.EmailNotice = say(r, "account.notice.email_changed")
	case "export_started":
		data.Notice = say(r, "account.notice.export_started")
	case "export_running":
		data.Notice = say(r, "account.notice.export_running")
	case "workspace_renamed":
		data.WorkspaceNotice = say(r, "account.workspace.saved")
	case "workspace_invalid":
		data.WorkspaceError = say(r, "account.workspace.invalid", uitext.Args{"Max": auth.MaxWorkspaceName})
	}
	// The latest export's state, if there has ever been one. A missing
	// row is the normal case, not a problem.
	if job, err := s.surveys.LatestExportJob(r.Context(), info.WorkspaceID); err == nil {
		data.Export = viewExportJob(text(r), job, s.clock.Now())
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, r, "read latest export", err)
		return
	}
	render(w, r, http.StatusOK, templates.Account(info.Email, info.WorkspaceName, info.CSRFToken, data))
}

func (s *server) accountDelete(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	if err := s.auth.DeleteAccount(r.Context(), info.UserID); err != nil {
		s.logger.Error("account deletion failed", "error", err)
		render(w, r, http.StatusInternalServerError, templates.ErrorPage(
			say(r, "error.generic.title"), say(r, "account.error.delete")))
		return
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/goodbye", http.StatusSeeOther)
}

func (s *server) goodbye(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, templates.Goodbye())
}

// healthz reports process + database liveness. The endpoint half of
// M1-T4, shipped early because the compose healthcheck and later uptime
// checks both want it.
func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	if !s.livenessOK(r.Context()) {
		http.Error(w, "db unreachable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok")) //nolint:errcheck
}

// healthProbeTTL is how long a liveness result is reused. This endpoint is
// unauthenticated, so without a cache a flood of /health hits would each
// open a DB round-trip; a short reuse window keeps probes honest while
// bounding the DB cost of abuse.
const healthProbeTTL = time.Second

// livenessOK reports database liveness, reusing the last probe within
// healthProbeTTL rather than pinging on every request.
func (s *server) livenessOK(ctx context.Context) bool {
	now := s.clock.Now()
	s.healthMu.Lock()
	if !s.healthCheckedAt.IsZero() && now.Sub(s.healthCheckedAt) < healthProbeTTL {
		ok := s.healthOK
		s.healthMu.Unlock()
		return ok
	}
	s.healthMu.Unlock()

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ok := s.pool.Ping(pingCtx) == nil

	s.healthMu.Lock()
	s.healthCheckedAt = now
	s.healthOK = ok
	s.healthMu.Unlock()
	return ok
}

// accountWorkspace renames the signed in creator's workspace: the name
// respondents read as who runs the survey. The workspace is the
// session's, never one named by the form.
func (s *server) accountWorkspace(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	err := s.auth.RenameWorkspace(r.Context(), info.WorkspaceID, r.PostFormValue("name"))
	switch {
	case errors.Is(err, auth.ErrInvalidWorkspaceName):
		http.Redirect(w, r, "/account?notice=workspace_invalid", http.StatusSeeOther)
	case err != nil:
		s.internalError(w, r, "rename workspace", err)
	default:
		http.Redirect(w, r, "/account?notice=workspace_renamed", http.StatusSeeOther)
	}
}
