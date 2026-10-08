package http

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

// Workspace suspension (ADR-0018, its safeguards). An operator suspends a
// workspace that misuses the service, a survey that impersonates somebody
// first among them, and lifts the suspension to put everything back.
// Nothing is erased and the purge does not start counting.
//
// What a suspension stops is decided in four places, one for each kind
// of thing it stops:
//
//   - answering: domain.SurveyState.AcceptsResponses, which every
//     respondent path asks, refuses a suspended workspace's surveys, and
//     respondUnavailable draws them as Earful's plain page;
//   - pictures: the query behind the public picture address serves none
//     of a suspended workspace's;
//   - AI: the meter, which every AI feature asks before it spends,
//     refuses with ai.ErrWorkspaceSuspended;
//   - the creator's own actions that reach respondents, publishing,
//     reopening and sending invitations, are routed through
//     refuseWhileSuspended.
//
// Everything else a creator does goes on as before: they sign in, read
// their results, edit drafts, and export their workspace.

// refuseWhileSuspended holds an action that would put a suspended
// workspace's survey in front of anybody, and says which it was by the
// message refused. It sits inside requireAuth, where the session has said
// whether the workspace is suspended.
func (s *server) refuseWhileSuspended(refused uitext.ID, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := authFrom(r.Context())
		if ok && info.WorkspaceSuspended {
			back := "/dashboard"
			if id, err := uuid.Parse(r.PathValue("surveyID")); err == nil {
				back = "/surveys/" + id.String()
			} else if strings.HasPrefix(r.URL.Path, "/account/style") {
				back = "/account/style"
			}
			// The page says what the notice would, and whom to ask, so it
			// goes without.
			r = r.WithContext(templates.WithoutSuspensionNotice(r.Context()))
			render(w, r, http.StatusForbidden, templates.WorkspaceSuspended(
				info.Email, info.CSRFToken, back, say(r, refused), operatorContact(s.cfg)))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// suspensionNotice tells every page of a suspended workspace's creator
// what is held and whom to ask, under the header.
func (s *server) suspensionNotice(r *http.Request) *http.Request {
	info, ok := authFrom(r.Context())
	if !ok || !info.WorkspaceSuspended {
		return r
	}
	return r.WithContext(templates.WithSuspension(r.Context(), operatorContact(s.cfg)))
}

// respondSuspended is what anybody following a link to a suspended
// workspace's survey sees: Earful's own page, with none of the survey's
// style, since the style may be what it was suspended for. It says no
// more than that the survey cannot be answered now.
func (s *server) respondSuspended(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusGone, templates.RespondUnavailable(
		"",
		say(r, "respond.refused.suspended.title"),
		say(r, "respond.refused.suspended.body"), domain.Style{}))
}

// The operator's page: /admin/suspensions lists the suspended workspaces
// and finds others through a member's address, as the other support
// tools find an account. Suspending takes a reason; both suspending and
// lifting are written to the log with who did it, by id.

func (s *server) adminSuspensionsPage(w http.ResponseWriter, r *http.Request) {
	data := templates.SuspensionsData{}
	switch r.URL.Query().Get("notice") {
	case "suspended":
		data.Notice = say(r, "admin.suspensions.notice.suspended")
	case "lifted":
		data.Notice = say(r, "admin.suspensions.notice.lifted")
	}
	s.renderSuspensions(w, r, http.StatusOK, strings.TrimSpace(r.URL.Query().Get("email")), data)
}

func (s *server) adminSuspend(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	address := strings.TrimSpace(r.PostFormValue("email"))
	reason := strings.TrimSpace(r.PostFormValue("reason"))

	workspaceID, err := uuid.Parse(r.PostFormValue("workspace"))
	if err != nil {
		s.renderSuspensions(w, r, http.StatusUnprocessableEntity, address,
			templates.SuspensionsData{Error: say(r, "admin.suspensions.error.workspace")})
		return
	}
	if reason == "" {
		s.renderSuspensions(w, r, http.StatusUnprocessableEntity, address, templates.SuspensionsData{
			Error: say(r, "admin.suspensions.error.reason"), ReasonFor: workspaceID.String(),
		})
		return
	}
	if len([]rune(reason)) > maxSuspensionReason {
		s.renderSuspensions(w, r, http.StatusUnprocessableEntity, address, templates.SuspensionsData{
			Error:     say(r, "admin.suspensions.error.reason_long", uitext.Args{"Max": maxSuspensionReason}),
			ReasonFor: workspaceID.String(), Reason: reason,
		})
		return
	}

	err = s.surveys.SuspendWorkspace(r.Context(), workspaceID, reason, info.UserID, s.clock.Now())
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.renderSuspensions(w, r, http.StatusUnprocessableEntity, address,
			templates.SuspensionsData{Error: say(r, "admin.suspensions.error.workspace")})
		return
	case errors.Is(err, store.ErrAlreadySuspended):
		s.renderSuspensions(w, r, http.StatusUnprocessableEntity, address,
			templates.SuspensionsData{Error: say(r, "admin.suspensions.error.already")})
		return
	case err != nil:
		s.internalError(w, r, "suspend workspace", err)
		return
	}
	// The record of who suspended a workspace. Ids only: the reason, the
	// workspace's name and its members' addresses stay in the database,
	// where the page shows them, and out of logs.
	s.logger.Info("workspace suspended", "workspace", workspaceID, "by", info.UserID)

	// The workspace is in the list now; the search that found it would
	// only show it a second time.
	http.Redirect(w, r, "/admin/suspensions?notice=suspended", http.StatusSeeOther)
}

func (s *server) adminLiftSuspension(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	address := strings.TrimSpace(r.PostFormValue("email"))

	workspaceID, err := uuid.Parse(r.PostFormValue("workspace"))
	if err != nil {
		s.renderSuspensions(w, r, http.StatusUnprocessableEntity, address,
			templates.SuspensionsData{Error: say(r, "admin.suspensions.error.workspace")})
		return
	}
	err = s.surveys.LiftWorkspaceSuspension(r.Context(), workspaceID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.renderSuspensions(w, r, http.StatusUnprocessableEntity, address,
			templates.SuspensionsData{Error: say(r, "admin.suspensions.error.workspace")})
		return
	case errors.Is(err, store.ErrNotSuspended):
		s.renderSuspensions(w, r, http.StatusUnprocessableEntity, address,
			templates.SuspensionsData{Error: say(r, "admin.suspensions.error.not_suspended")})
		return
	case err != nil:
		s.internalError(w, r, "lift workspace suspension", err)
		return
	}
	s.logger.Info("workspace suspension lifted", "workspace", workspaceID, "by", info.UserID)

	http.Redirect(w, r, "/admin/suspensions?"+url.Values{
		"email": {address}, "notice": {"lifted"},
	}.Encode(), http.StatusSeeOther)
}

// maxSuspensionReason bounds what an operator writes: a sentence or two
// for whoever reads the page next, not a case file.
const maxSuspensionReason = 500

// renderSuspensions lists the suspended workspaces, and the workspaces
// of the account whose address was given, around whatever the caller set.
func (s *server) renderSuspensions(w http.ResponseWriter, r *http.Request, status int, address string, data templates.SuspensionsData) {
	info, _ := authFrom(r.Context())
	loc := text(r)

	suspended, err := s.surveys.SuspendedWorkspaces(r.Context())
	if err != nil {
		s.internalError(w, r, "list suspended workspaces", err)
		return
	}
	for _, ws := range suspended {
		data.Suspended = append(data.Suspended, templates.SuspendedWorkspace{
			ID:     ws.ID.String(),
			Name:   ws.Name,
			Member: ws.MemberEmail,
			Since:  loc.Day(ws.SuspendedAt),
			By:     ws.SuspendedByEmail,
			Reason: ws.Reason,
		})
	}

	data.Email = address
	if address != "" {
		found, err := s.surveys.WorkspacesForSuspension(r.Context(), strings.ToLower(address))
		if err != nil {
			s.internalError(w, r, "list workspaces for suspension", err)
			return
		}
		data.Searched = true
		for _, ws := range found {
			data.Found = append(data.Found, templates.FoundWorkspace{
				ID: ws.ID.String(), Name: ws.Name, Suspended: ws.Suspended,
			})
		}
	}
	render(w, r, status, templates.AdminSuspensions(info.Email, info.CSRFToken, data))
}
