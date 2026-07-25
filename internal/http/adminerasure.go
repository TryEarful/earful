package http

import (
	"net/http"
	"strings"

	"github.com/TryEarful/earful/internal/purge"
	"github.com/TryEarful/earful/web/templates"
)

// The erasure fast-path (M8-T3). A GDPR erasure request has to complete
// within 24 hours, which the ordinary 30-day purge cannot promise, so
// support can erase one subject now.
//
// Two properties matter more than convenience here:
//
//   - It is two steps. Looking up an address shows exactly what would go;
//     erasing is a separate, confirmed action. An irreversible thing done
//     on someone else's behalf should never be one click.
//   - It is honest about anonymous responses. They contain no personal
//     data and cannot be found by address — that is the anonymity promise
//     working, not a gap, and the page says so rather than leaving
//     support to wonder.

func (s *server) adminErasurePage(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	data := templates.ErasureData{}

	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if email != "" {
		subject, err := purge.PreviewSubject(r.Context(), s.pool, email)
		if err != nil {
			s.internalError(w, r, "preview erasure subject", err)
			return
		}
		data.Searched = true
		data.Subject = viewSubject(subject)
	}
	render(w, r, http.StatusOK, templates.AdminErasure(info.Email, info.CSRFToken, data))
}

func (s *server) adminErasureRun(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	email := strings.TrimSpace(r.PostFormValue("email"))
	if email == "" {
		http.Redirect(w, r, "/admin/erasure", http.StatusSeeOther)
		return
	}

	report, err := purge.EraseSubject(r.Context(), s.pool, email, s.clock.Now())
	if err != nil {
		s.internalError(w, r, "erase subject", err)
		return
	}
	// Counts only, and the address is not written to the log: an erasure
	// record that names the person erased is its own problem.
	s.logger.Info("erasure completed", "rows", report.Total(), "by", info.UserID)

	render(w, r, http.StatusOK, templates.AdminErasure(info.Email, info.CSRFToken, templates.ErasureData{
		Done:      true,
		ErasedRow: report.Total(),
	}))
}

func viewSubject(subject purge.Subject) templates.SubjectView {
	return templates.SubjectView{
		Email:         subject.Email,
		Found:         subject.Found(),
		HasAccount:    subject.HasAccount,
		Workspaces:    subject.Workspaces,
		Surveys:       subject.Surveys,
		ParticipantIn: subject.ParticipantIn,
		Responses:     subject.Responses,
		Suppressed:    subject.Suppressed,
	}
}
