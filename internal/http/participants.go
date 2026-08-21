package http

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/antibot"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/web/templates"
)

// respondAsParticipant carries the invite context into the shared
// respondent renderer: the form posts back to the personal link, and the
// disclosure names who is answering.
func respondAsParticipant(token string, p store.ResolvedParticipant) *participantContext {
	return &participantContext{token: token, email: p.Email}
}

type participantContext struct {
	token string
	email string
}

// participantsImport ingests pasted addresses and/or an uploaded CSV into
// a survey's participant list (M4-T3).
func (s *server) participantsImport(w http.ResponseWriter, r *http.Request) {
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	if survey.IsAnonymous {
		s.surveyNotFound(w, r)
		return
	}

	raw := r.PostFormValue("emails")
	if file, _, err := r.FormFile("csv"); err == nil {
		defer file.Close()
		content, err := io.ReadAll(io.LimitReader(file, 1<<20))
		if err != nil {
			s.internalError(w, r, "read csv upload", err)
			return
		}
		raw += "\n" + string(content)
	}

	added, invalid, err := s.surveys.ImportParticipants(r.Context(), survey.ID, raw)
	if errors.Is(err, store.ErrImportTooLarge) {
		s.renderSurveyPage(w, r, err.Error(), "")
		return
	}
	if err != nil {
		s.internalError(w, r, "import participants", err)
		return
	}
	notice := "Added " + strconv.Itoa(added) + " participants."
	if added == 1 {
		notice = "Added 1 participant."
	}
	if invalid > 0 {
		notice += " Skipped " + strconv.Itoa(invalid) + " entries that didn't look like email addresses."
	}
	s.renderSurveyPage(w, r, "", notice)
}

// participantsSend drips invites under the workspace cap (M4-T4).
func (s *server) participantsSend(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	if survey.IsAnonymous {
		s.surveyNotFound(w, r)
		return
	}
	if survey.StatusAt(s.clock.Now()) != domain.StatusOpen {
		s.renderSurveyPage(w, r, "Publish the survey (and keep it open) before sending invites — the links would lead to a dead page.", "")
		return
	}

	result, err := s.invites.SendPending(r.Context(), info.WorkspaceID, survey.ID, survey.Title, info.WorkspaceName)
	if err != nil {
		s.internalError(w, r, "send invites", err)
		return
	}
	notice := "Sent " + strconv.Itoa(result.Sent) + " invites."
	if result.Sent == 1 {
		notice = "Sent 1 invite."
	}
	if result.Failed > 0 {
		notice += " " + strconv.Itoa(result.Failed) + " failed and will be retried."
	}
	if result.Remaining > 0 {
		notice += " " + strconv.Itoa(result.Remaining) + " are waiting on the hourly sending cap and will go out on the next run."
	}
	s.renderSurveyPage(w, r, "", notice)
}

// participantRespondPage serves the personal invite link (M4-T3): the
// token is the credential, one submission each.
func (s *server) participantRespondPage(w http.ResponseWriter, r *http.Request) {
	participant, survey, version, ok := s.loadParticipantSurvey(w, r)
	if !ok {
		return
	}
	s.recordStart(r, survey.ID)
	s.renderRespondPage(w, r, survey, version,
		domain.Submission{Answers: map[string]domain.AnswerValue{}}, nil, "",
		respondAsParticipant(r.PathValue("token"), participant))
}

func (s *server) participantRespondSubmit(w http.ResponseWriter, r *http.Request) {
	// The version this returns is the survey's current one; the submission
	// is scored against the version actually served to this respondent,
	// loaded by id from the form below.
	participant, survey, _, ok := s.loadParticipantSurvey(w, r)
	if !ok {
		return
	}
	asParticipant := respondAsParticipant(r.PathValue("token"), participant)

	// Same honeypot and reading-time checks as the anonymous path; no
	// ALTCHA — the 128-bit token already gates access, and the
	// one-submission index caps abuse at one row.
	if r.PostFormValue("website") != "" {
		s.logAbuse(r, "honeypot")
		render(w, r, http.StatusOK, templates.RespondThanks(survey.Title, survey.IsAnonymous))
		return
	}
	servedID, err := uuid.Parse(r.PostFormValue("version_id"))
	if err != nil {
		s.respondNotFound(w, r)
		return
	}
	version, err := s.surveys.ServedVersionByID(r.Context(), survey.ID, servedID)
	if errors.Is(err, store.ErrNotFound) {
		s.respondNotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, r, "load served version", err)
		return
	}
	switch err := s.formTokens.Check(survey.ID.String(), r.PostFormValue("form_ts"), minFillTime); {
	case errors.Is(err, antibot.ErrFormTooFast):
		s.logAbuse(r, "too_fast")
		s.renderRespondPage(w, r, survey, version, parseSubmission(r, version.Questions), nil,
			"That was quick! Take a moment to review your answers, then submit again.", asParticipant)
		return
	case err != nil:
		s.logAbuse(r, "bad_form_token")
		s.renderRespondPage(w, r, survey, version, parseSubmission(r, version.Questions), nil,
			"This form had been open a while. Your answers are still here — review and submit again.", asParticipant)
		return
	}

	submission := parseSubmission(r, version.Questions)
	if problems := submission.Validate(version.Questions); len(problems) > 0 {
		s.renderRespondPage(w, r, survey, version, submission, problems, "", asParticipant)
		return
	}

	_, err = s.surveys.SubmitAnswers(r.Context(), survey.ID, version, &participant.ID,
		submission.Answers, durationFrom(r, s.clock.Now()), s.clock.Now())
	if errors.Is(err, store.ErrAlreadySubmitted) {
		render(w, r, http.StatusOK, templates.RespondAlreadySubmitted(survey.Title))
		return
	}
	if err != nil {
		s.internalError(w, r, "submit participant response", err)
		return
	}
	s.recordCompletion(r, survey.ID, version.Questions, submission)
	render(w, r, http.StatusOK, templates.RespondThanks(survey.Title, survey.IsAnonymous))
}

// loadParticipantSurvey resolves an invite token to its participant and
// answerable survey, rendering the right page and returning false when
// answering cannot proceed.
func (s *server) loadParticipantSurvey(w http.ResponseWriter, r *http.Request) (store.ResolvedParticipant, store.PublicSurvey, store.ServedVersion, bool) {
	fail := func() (store.ResolvedParticipant, store.PublicSurvey, store.ServedVersion, bool) {
		return store.ResolvedParticipant{}, store.PublicSurvey{}, store.ServedVersion{}, false
	}

	participant, err := s.surveys.ParticipantByToken(r.Context(), r.PathValue("token"))
	if errors.Is(err, store.ErrNotFound) {
		s.respondNotFound(w, r)
		return fail()
	}
	if err != nil {
		s.internalError(w, r, "resolve participant", err)
		return fail()
	}
	if participant.SubmittedAt != nil {
		survey, err := s.surveys.PublicSurvey(r.Context(), participant.SurveyID)
		title := ""
		if err == nil {
			title = survey.Title
		}
		render(w, r, http.StatusOK, templates.RespondAlreadySubmitted(title))
		return fail()
	}

	survey, err := s.surveys.PublicSurvey(r.Context(), participant.SurveyID)
	if errors.Is(err, store.ErrNotFound) {
		s.respondNotFound(w, r)
		return fail()
	}
	if err != nil {
		s.internalError(w, r, "load survey for participant", err)
		return fail()
	}
	if !survey.State().AcceptsResponses(s.clock.Now()) {
		s.respondUnavailable(w, r, survey)
		return fail()
	}
	version, err := s.surveys.LatestServedVersion(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "load served version", err)
		return fail()
	}
	return participant, survey, version, true
}

// emailWebhook receives ESP events. The secret lives in the URL path —
// the way Brevo delivers shared-secret webhooks — and a missing or wrong
// secret is a plain 404, indistinguishable from the route not existing.
func (s *server) emailWebhook(w http.ResponseWriter, r *http.Request) {
	secret := s.cfg.EmailWebhookSecret
	if secret == "" ||
		subtle.ConstantTimeCompare([]byte(r.PathValue("secret")), []byte(secret)) != 1 {
		http.NotFound(w, r)
		return
	}
	s.emailSender.HandleWebhook(w, r)
}
