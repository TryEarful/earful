package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/email"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

// The copy of an account's data sent when the account closes. It is the
// workspace export, built by the same builder in the same format, with
// two differences that follow from there being no account left: the
// link is emailed and is a bearer capability, and it lives for
// closureExportTTL rather than a day, since the person has no account
// page to come back to.

// closureExportTTL is how long the emailed link works, counted from the
// moment the account closes. The purge keys on each row's expires_at, and
// the 30-day soft-delete window that removes a closed workspace's jobs is
// longer, so the archive lives exactly this long.
const closureExportTTL = 7 * 24 * time.Hour

// closureExport is a queued closure export and what is needed to finish
// it after the request has been answered.
type closureExport struct {
	job           store.ExportJob
	token         string
	to            string
	workspaceName string
	// text words the email in the language the account was closed in.
	text uitext.Localizer
}

// queueClosureExport records the job and its token's hash. It returns
// nil without an error when the workspace already has a closure export,
// which only a second submit of the same form racing the first can
// reach: that copy is already on its way.
func (s *server) queueClosureExport(r *http.Request, info auth.AuthInfo) (*closureExport, error) {
	now := s.clock.Now()
	token := auth.NewToken()
	job, created, err := s.surveys.CreateClosureExportJob(r.Context(), info.WorkspaceID, info.UserID,
		auth.HashToken(token), now, now.Add(closureExportTTL))
	if err != nil || !created {
		return nil, err
	}
	return &closureExport{
		job:           job,
		token:         token,
		to:            info.Email,
		workspaceName: info.WorkspaceName,
		text:          text(r),
	}, nil
}

// startClosureExport builds the archive off the request and writes to
// the person with the link, or with why there is none: they can no
// longer sign in to see a failure anywhere else.
func (s *server) startClosureExport(c closureExport) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		claimed, err := s.surveys.ClaimExportJob(ctx, c.job.ID)
		if err != nil || !claimed {
			return
		}
		archive, err := s.buildWorkspaceArchive(ctx, c.job.WorkspaceID, c.workspaceName)
		now := s.clock.Now()
		if err != nil {
			s.logger.Error("closure export failed", "error", err)
			stored, reason := exportFailed, c.text.T("email.closure.failed.generic")
			if errors.Is(err, errExportTooLarge) {
				stored, reason = exportTooLarge, c.text.T("email.closure.failed.large")
			}
			if failErr := s.surveys.FailExportJob(ctx, c.job.ID, stored, now); failErr != nil {
				s.logger.Error("marking closure export failed did not work", "error", failErr)
			}
			s.sendClosureEmail(ctx, email.Message{
				To:      c.to,
				Subject: c.text.T("email.closure.failed.subject"),
				Text:    c.text.T("email.closure.failed.body", uitext.Args{"Reason": reason}),
			})
			return
		}
		expires := *c.job.ExpiresAt
		if err := s.surveys.FinishExportJob(ctx, c.job.ID, archive, now, expires); err != nil {
			s.logger.Error("storing closure export failed", "error", err)
			s.sendClosureEmail(ctx, email.Message{
				To:      c.to,
				Subject: c.text.T("email.closure.failed.subject"),
				Text: c.text.T("email.closure.failed.body",
					uitext.Args{"Reason": c.text.T("email.closure.failed.generic")}),
			})
			return
		}
		s.sendClosureEmail(ctx, email.Message{
			To:      c.to,
			Subject: c.text.T("email.closure.subject"),
			Text: c.text.T("email.closure.body", uitext.Args{
				"Link":    s.cfg.BaseURL + "/exports/closure/" + c.token,
				"Expires": c.text.DateTime(expires.UTC()),
			}),
		})
	}()
}

func (s *server) sendClosureEmail(ctx context.Context, msg email.Message) {
	if err := s.emailSender.Send(ctx, msg); err != nil {
		s.logger.Error("sending the closure export email failed", "error", err)
	}
}

// closureDownload serves a closure export to whoever holds its token.
// Every way of not finding one (a wrong token, an expired or failed
// export, an account export's id) answers the same 404, so the route
// says nothing about which tokens exist.
func (s *server) closureDownload(w http.ResponseWriter, r *http.Request) {
	archive, err := s.surveys.ClosureArchive(r.Context(), auth.HashToken(r.PathValue("token")), s.clock.Now())
	if errors.Is(err, store.ErrNotFound) {
		render(w, r, http.StatusNotFound, templates.ErrorPage(say(r, "export.closure.missing.title"),
			say(r, "export.closure.missing.body")))
		return
	}
	if err != nil {
		s.internalError(w, r, "read closure archive", err)
		return
	}
	// The archive is somebody's whole workspace, fetched with a bearer
	// link: no cache along the way may keep a copy.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="earful-workspace-export.zip"`)
	if _, err := w.Write(archive); err != nil {
		s.logger.Debug("closure export download interrupted", "error", err)
	}
}
