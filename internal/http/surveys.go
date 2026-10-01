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
	form := newSurveyForm(r)

	closeAt, err := parseCloseDate(form.CloseAt)
	if err != nil {
		s.renderNewSurvey(w, r, form, sayErrorAlone(r, err))
		return
	}
	// A description or attached files are read only where AI is offered;
	// on an instance without it the fields are absent and posted ones are
	// ignored.
	if (form.Prompt != "" || hasUploads(r)) && s.canGenerate() {
		s.surveyCreateFromPrompt(w, r, form, closeAt)
		return
	}
	survey, err := s.surveys.Create(r.Context(), info.WorkspaceID, info.UserID,
		form.Title, form.Anonymous, closeAt)
	if err != nil {
		// Where AI is offered an empty title is allowed with a description,
		// so the error names both ways out.
		if errors.Is(err, domain.ErrEmptyTitle) && s.canGenerate() {
			s.renderNewSurvey(w, r, form, sentence(say(r, "survey.error.untitled_or_described")))
			return
		}
		if isUserError(err) {
			s.renderNewSurvey(w, r, form, sayErrorAlone(r, err))
			return
		}
		s.internalError(w, r, "create survey", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String(), http.StatusSeeOther)
}

// newSurveyForm reads the new-survey form, keeping what was typed so an
// error can hand it back unchanged.
func newSurveyForm(r *http.Request) templates.NewSurveyData {
	return templates.NewSurveyData{
		Title:     r.PostFormValue("title"),
		Prompt:    strings.TrimSpace(r.PostFormValue("prompt")),
		Anonymous: r.PostFormValue("anonymity") == "anonymous",
		CloseAt:   r.PostFormValue("close_at"),
	}
}

func (s *server) newSurveyPage(w http.ResponseWriter, r *http.Request) {
	s.renderNewSurvey(w, r, templates.NewSurveyData{Anonymous: true}, "")
}

func (s *server) renderNewSurvey(w http.ResponseWriter, r *http.Request, data templates.NewSurveyData, errMsg string) {
	info, _ := authFrom(r.Context())
	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusUnprocessableEntity
	}
	data.AIEnabled = s.canGenerate()
	data.Error = errMsg
	render(w, r, status, templates.NewSurvey(info.Email, info.WorkspaceName, info.CSRFToken, data))
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
	} else if q.Get("notice") == "thanks" {
		notice = say(r, "editor.notice.thanks")
	} else if added, err := strconv.Atoi(q.Get("added")); err == nil && added >= 0 {
		// A survey drafted from a description redirects here with what
		// the run added and skipped, so the notice survives the redirect.
		skipped, _ := strconv.Atoi(q.Get("skipped"))
		notice = generationNotice(text(r), added, max(skipped, 0))
	}
	s.renderSurveyPage(w, r, "", notice)
}

func (s *server) renderSurveyPage(w http.ResponseWriter, r *http.Request, errMsg, notice string) {
	s.renderSurveyEditor(w, r, errMsg, notice, nil, "")
}

// renderEditor is renderSurveyPage with the drafting panel's prompt
// filled in, for a refused run that hands back what was typed.
func (s *server) renderEditor(w http.ResponseWriter, r *http.Request, errMsg, notice, generatePrompt string) {
	s.renderSurveyEditor(w, r, errMsg, notice, nil, generatePrompt)
}

// renderSurveyEditor draws the editor. thanks, when given, is what the
// creator just typed into the thank you page form, shown back to them
// beside the error that refused it rather than lost; generatePrompt is
// the drafting panel's prompt, handed back after a refused run.
func (s *server) renderSurveyEditor(w http.ResponseWriter, r *http.Request, errMsg, notice string, thanks *templates.ThanksFormView, generatePrompt string) {
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

	changed, err := s.surveys.HasUnpublishedChanges(r.Context(), survey.ID, draft)
	if err != nil {
		s.internalError(w, r, "compare draft", err)
		return
	}

	data := templates.SurveyEditorData{
		Origin:        s.cfg.BaseURL,
		DraftChanged:  changed,
		Survey:        viewSurvey(text(r), survey, s.clock.Now()),
		Questions:     draft.Questions,
		Versions:      viewVersions(text(r), versions),
		ResponseCount: responses,
		AIEnabled:     s.canGenerate(),
		Thanks: templates.ThanksFormView{
			Message:   draft.Thanks.Message,
			LinkLabel: draft.Thanks.LinkLabel,
			LinkURL:   draft.Thanks.LinkURL,
		},
		Error:          errMsg,
		Notice:         notice,
		GeneratePrompt: generatePrompt,
	}
	if thanks != nil {
		data.Thanks = *thanks
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
		s.renderSurveyPage(w, r, sayErrorAlone(r, err), "")
		return
	}
	if err := s.surveys.UpdateSettings(r.Context(), info.WorkspaceID, survey.ID, r.PostFormValue("title"), closeAt); err != nil {
		if isUserError(err) {
			s.renderSurveyPage(w, r, sayErrorAlone(r, err), "")
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
		s.renderSurveyPage(w, r, sayErrorAlone(r, err), "")
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
		s.renderSurveyPage(w, r, sayErrorAlone(r, err), "")
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
		s.renderSurveyPage(w, r, sayErrorAlone(r, err), "")
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
		s.renderSurveyPage(w, r, sayErrorAlone(r, err), "")
		return
	}
	s.saveDraftAndRedirect(w, r, survey.ID, info.UserID, draft)
}

// surveyThanks saves the thank you page. It is a draft change like a
// question edit: it appends a revision, and respondents see it only once
// the next version is published (ADR-0001), so a live survey's thank you
// page never changes under a respondent who is answering it.
func (s *server) surveyThanks(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	typed := templates.ThanksFormView{
		Message:   r.PostFormValue("thanks_message"),
		LinkLabel: r.PostFormValue("thanks_link_label"),
		LinkURL:   r.PostFormValue("thanks_link_url"),
	}
	thanks, err := domain.NewThankYou(typed.Message, typed.LinkLabel, typed.LinkURL)
	if err == nil {
		err = draft.SetThanks(thanks)
	}
	if err != nil {
		s.renderSurveyEditor(w, r, sayErrorAlone(r, err), "", &typed, "")
		return
	}
	if err := s.surveys.SaveDraft(r.Context(), survey.ID, info.UserID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String()+"?notice=thanks", http.StatusSeeOther)
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
		s.renderSurveyPage(w, r, sayErrorAlone(r, err), "")
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
		// Read only here: the add form keeps the box, hidden, while
		// another type is picked, and a box ticked before the type
		// changed means nothing for the type chosen.
		q.AllowOther = r.PostFormValue("allow_other") == "on"
	}
	if q.Type.NeedsScale() {
		q.ScaleMin, _ = strconv.Atoi(r.PostFormValue("scale_min"))
		q.ScaleMax, _ = strconv.Atoi(r.PostFormValue("scale_max"))
		if q.ScaleMax == 0 {
			q.ScaleMax = 5
		}
	}
	if q.Type == domain.Number {
		// Both limits are required. One missing or not a whole number
		// leaves both at zero, which validation refuses, rather than
		// a zero quietly standing in for what the author left out.
		low, lowErr := strconv.Atoi(strings.TrimSpace(r.PostFormValue("number_min")))
		high, highErr := strconv.Atoi(strings.TrimSpace(r.PostFormValue("number_max")))
		if lowErr == nil && highErr == nil {
			q.ScaleMin, q.ScaleMax = low, high
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
