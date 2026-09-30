package http

import (
	"errors"
	"github.com/TryEarful/earful/internal/uitext"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/web/templates"
)

// closeDateLayout is the value an <input type="date"> submits.
const closeDateLayout = "2006-01-02"

func (s *server) surveyList(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	surveys, err := s.surveys.List(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "list surveys", err)
		return
	}
	render(w, r, http.StatusOK, templates.Dashboard(
		info.Email, info.WorkspaceName, info.CSRFToken,
		templates.SurveyListData{Surveys: viewSurveys(text(r), surveys, s.clock.Now())},
	))
}

func (s *server) surveyCreate(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())

	closeAt, err := parseCloseDate(r.PostFormValue("close_at"))
	if err != nil {
		s.renderNewSurvey(w, r, sayError(r, err))
		return
	}
	survey, err := s.surveys.Create(r.Context(), info.WorkspaceID, info.UserID,
		r.PostFormValue("title"), r.PostFormValue("anonymity") == "anonymous", closeAt)
	if err != nil {
		if isUserError(err) {
			s.renderNewSurvey(w, r, sayError(r, err))
			return
		}
		s.internalError(w, r, "create survey", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String(), http.StatusSeeOther)
}

func (s *server) newSurveyPage(w http.ResponseWriter, r *http.Request) {
	s.renderNewSurvey(w, r, "")
}

func (s *server) renderNewSurvey(w http.ResponseWriter, r *http.Request, errMsg string) {
	info, _ := authFrom(r.Context())
	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusUnprocessableEntity
	}
	render(w, r, status, templates.NewSurvey(info.Email, info.WorkspaceName, info.CSRFToken, errMsg))
}

// surveyPage is the editor: draft questions, status, versions. A
// publish redirects here and names its outcome in the query, so the
// notice survives the redirect and a reload does not publish again.
func (s *server) surveyPage(w http.ResponseWriter, r *http.Request) {
	notice := ""
	q := r.URL.Query()
	if n, err := strconv.Atoi(q.Get("published")); err == nil && n > 0 {
		notice = say(r, "editor.notice.published", uitext.Args{"Version": n})
	} else if q.Get("notice") == "unchanged" {
		notice = say(r, "editor.notice.unchanged")
	}
	s.renderSurveyPage(w, r, "", notice)
}

func (s *server) renderSurveyPage(w http.ResponseWriter, r *http.Request, errMsg, notice string) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	versions, err := s.surveys.Versions(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "list versions", err)
		return
	}
	responses, err := s.surveys.ResponseCount(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "count responses", err)
		return
	}

	changed, err := s.surveys.HasUnpublishedChanges(r.Context(), survey.ID, draft.Questions)
	if err != nil {
		s.internalError(w, r, "compare draft", err)
		return
	}

	data := templates.SurveyEditorData{
		DraftChanged:  changed,
		Survey:        viewSurvey(text(r), survey, s.clock.Now()),
		Questions:     draft.Questions,
		Versions:      viewVersions(text(r), versions),
		ResponseCount: responses,
		AIEnabled:     s.canGenerate(),
		Error:         errMsg,
		Notice:        notice,
	}
	if !survey.IsAnonymous {
		participants, err := s.surveys.Participants(r.Context(), survey.ID)
		if err != nil {
			s.internalError(w, r, "list participants", err)
			return
		}
		for _, p := range participants {
			view := templates.ParticipantView{Email: p.Email, Status: participantStatus(text(r), p.Status())}
			if p.Status() == store.ParticipantPending {
				data.PendingCount++
			}
			data.Participants = append(data.Participants, view)
		}
	}

	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusUnprocessableEntity
	}
	render(w, r, status, templates.SurveyEditor(info.Email, info.WorkspaceName, info.CSRFToken, data))
}

func (s *server) surveySettings(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	closeAt, err := parseCloseDate(r.PostFormValue("close_at"))
	if err != nil {
		s.renderSurveyPage(w, r, sayError(r, err), "")
		return
	}
	if err := s.surveys.UpdateSettings(r.Context(), info.WorkspaceID, survey.ID, r.PostFormValue("title"), closeAt); err != nil {
		if isUserError(err) {
			s.renderSurveyPage(w, r, sayError(r, err), "")
			return
		}
		s.internalError(w, r, "update survey settings", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String(), http.StatusSeeOther)
}

// questionAdd appends a question to the draft. Every mutation here goes
// through SaveDraft, so each one appends a Draft Revision (M3-T2).
func (s *server) questionAdd(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}

	q := questionFromForm(r)
	q.IdentityID = uuid.NewString() // a new question starts a new identity
	if err := draft.Add(q); err != nil {
		s.renderSurveyPage(w, r, sayError(r, err), "")
		return
	}
	s.saveDraftAndRedirect(w, r, survey.ID, info.UserID, draft)
}

// questionUpdate rewords or reshapes a question. The identity comes from
// the URL and is preserved, which is what keeps results comparable across
// versions (ADR-0001) — the form cannot change it.
func (s *server) questionUpdate(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	if err := draft.Replace(r.PathValue("questionID"), questionFromForm(r)); err != nil {
		s.renderSurveyPage(w, r, sayError(r, err), "")
		return
	}
	s.saveDraftAndRedirect(w, r, survey.ID, info.UserID, draft)
}

func (s *server) questionDelete(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	if err := draft.Remove(r.PathValue("questionID")); err != nil {
		s.renderSurveyPage(w, r, sayError(r, err), "")
		return
	}
	s.saveDraftAndRedirect(w, r, survey.ID, info.UserID, draft)
}

func (s *server) questionMove(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	delta := 1
	if r.PostFormValue("direction") == "up" {
		delta = -1
	}
	if err := draft.Move(r.PathValue("questionID"), delta); err != nil {
		s.renderSurveyPage(w, r, sayError(r, err), "")
		return
	}
	s.saveDraftAndRedirect(w, r, survey.ID, info.UserID, draft)
}

func (s *server) surveyPublish(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	version, err := s.surveys.Publish(r.Context(), info.WorkspaceID, survey.ID, info.UserID, s.clock.Now())
	switch {
	case errors.Is(err, store.ErrNothingToPublish):
		http.Redirect(w, r, "/surveys/"+survey.ID.String()+"?notice=unchanged", http.StatusSeeOther)
		return
	case isUserError(err):
		s.renderSurveyPage(w, r, sayError(r, err), "")
		return
	case err != nil:
		s.internalError(w, r, "publish survey", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String()+"?published="+strconv.Itoa(version.Number), http.StatusSeeOther)
}

func (s *server) surveyClose(w http.ResponseWriter, r *http.Request) {
	s.setSurveyClosed(w, r, true)
}

func (s *server) surveyReopen(w http.ResponseWriter, r *http.Request) {
	s.setSurveyClosed(w, r, false)
}

func (s *server) setSurveyClosed(w http.ResponseWriter, r *http.Request, closed bool) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	if err := s.surveys.SetClosed(r.Context(), info.WorkspaceID, survey.ID, closed, s.clock.Now()); err != nil {
		s.internalError(w, r, "set survey closed", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String(), http.StatusSeeOther)
}

func (s *server) surveyDelete(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	if err := s.surveys.SoftDelete(r.Context(), info.WorkspaceID, survey.ID, s.clock.Now()); err != nil {
		s.internalError(w, r, "delete survey", err)
		return
	}
	http.Redirect(w, r, "/surveys", http.StatusSeeOther)
}

// surveyAudit renders the derived who-changed-what trail: draft saves and
// publishes, newest first (M3-T4).
func (s *server) surveyAudit(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	_, draftID, err := s.surveys.Draft(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "load draft", err)
		return
	}
	revisions, err := s.surveys.Revisions(r.Context(), draftID)
	if err != nil {
		s.internalError(w, r, "list revisions", err)
		return
	}
	versions, err := s.surveys.Versions(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "list versions", err)
		return
	}
	render(w, r, http.StatusOK, templates.SurveyAudit(info.Email, info.WorkspaceName, info.CSRFToken,
		templates.SurveyAuditData{
			Survey:   viewSurvey(text(r), survey, s.clock.Now()),
			Entries:  auditEntries(text(r), revisions, versions),
			Versions: viewVersions(text(r), versions),
		}))
}

// --- helpers -------------------------------------------------------------

func (s *server) loadSurvey(w http.ResponseWriter, r *http.Request) (store.Survey, bool) {
	info, _ := authFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("surveyID"))
	if err != nil {
		s.surveyNotFound(w, r)
		return store.Survey{}, false
	}
	survey, err := s.surveys.Get(r.Context(), info.WorkspaceID, id)
	if errors.Is(err, store.ErrNotFound) {
		s.surveyNotFound(w, r)
		return store.Survey{}, false
	}
	if err != nil {
		s.internalError(w, r, "get survey", err)
		return store.Survey{}, false
	}
	return survey, true
}

func (s *server) loadSurveyAndDraft(w http.ResponseWriter, r *http.Request) (store.Survey, domain.Draft, bool) {
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return store.Survey{}, domain.Draft{}, false
	}
	draft, _, err := s.surveys.Draft(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "load draft", err)
		return store.Survey{}, domain.Draft{}, false
	}
	return survey, draft, true
}

func (s *server) saveDraftAndRedirect(w http.ResponseWriter, r *http.Request, surveyID, userID uuid.UUID, draft domain.Draft) {
	if err := s.surveys.SaveDraft(r.Context(), surveyID, userID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+surveyID.String(), http.StatusSeeOther)
}

// surveyNotFound is deliberately identical for "no such survey" and
// "belongs to another workspace": distinguishing them would turn the id
// space into an oracle for other customers' data.
func (s *server) surveyNotFound(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusNotFound, templates.ErrorPage(
		say(r, "error.survey.title"),
		say(r, "error.survey.body")))
}

func (s *server) internalError(w http.ResponseWriter, r *http.Request, what string, err error) {
	s.logger.Error(what+" failed", "error", err)
	render(w, r, http.StatusInternalServerError, templates.ErrorPage(
		say(r, "error.generic.title"), say(r, "error.generic.body")))
}

func questionFromForm(r *http.Request) domain.Question {
	q := domain.Question{
		Type:     domain.QuestionType(r.PostFormValue("type")),
		Text:     strings.TrimSpace(r.PostFormValue("text")),
		Required: r.PostFormValue("required") == "on",
	}
	if q.Type.NeedsOptions() {
		for _, line := range strings.Split(r.PostFormValue("options"), "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				q.Options = append(q.Options, trimmed)
			}
		}
	}
	if q.Type.NeedsScale() {
		q.ScaleMin, _ = strconv.Atoi(r.PostFormValue("scale_min"))
		q.ScaleMax, _ = strconv.Atoi(r.PostFormValue("scale_max"))
		if q.ScaleMax == 0 {
			q.ScaleMax = 5
		}
	}
	return q
}

func parseCloseDate(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	day, err := time.Parse(closeDateLayout, raw)
	if err != nil {
		return nil, errors.New("that close date isn't a valid date")
	}
	// A close date means "through the end of that day".
	end := day.Add(24 * time.Hour)
	return &end, nil
}
