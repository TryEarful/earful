package http

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"

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
		s.renderSurveyPage(w, r, sayErrorAlone(r, err), "")
		return
	}
	if err != nil {
		s.internalError(w, r, "import participants", err)
		return
	}
	notice := sayN(r, "editor.notice.added", added)
	if invalid > 0 {
		notice += " " + sayN(r, "editor.notice.skipped", invalid)
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
		s.renderSurveyPage(w, r, say(r, "editor.error.unpublished"), "")
		return
	}

	result, err := s.invites.SendPending(r.Context(), info.WorkspaceID, survey.ID, survey.Title, info.WorkspaceName)
	if err != nil {
		s.internalError(w, r, "send invites", err)
		return
	}
	notice := sayN(r, "editor.notice.sent", result.Sent)
	if result.Failed > 0 {
		notice += " " + sayN(r, "editor.notice.failed", result.Failed)
	}
	if result.Remaining > 0 {
		notice += " " + sayN(r, "editor.notice.waiting", result.Remaining)
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
	s.applyLanguage(r, &version)
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
		render(w, r, http.StatusOK, templates.RespondThanks(s.unrecordedThanks(r, survey)))
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
	// As on the anonymous path: read and re-shown in the language the
	// form was rendered in, validated and stored in the creator's wording.
	shown := s.shownVersion(r, version)
	asSubmitted := parseSubmission(r, shown.Questions)

	switch err := s.formTokens.Check(survey.ID.String(), r.PostFormValue("form_ts"), minFillTime); {
	case errors.Is(err, antibot.ErrFormTooFast):
		s.logAbuse(r, "too_fast")
		s.renderRespondPage(w, r, survey, shown, asSubmitted, nil,
			say(r, "respond.notice.quick"), asParticipant)
		return
	case err != nil:
		s.logAbuse(r, "bad_form_token")
		s.renderRespondPage(w, r, survey, shown, asSubmitted, nil,
			say(r, "respond.notice.stale"), asParticipant)
		return
	}

	submission := canonicalAnswers(asSubmitted, shown.Questions, version.Questions)
	if problems := submission.Validate(version.Questions); len(problems) > 0 {
		s.renderRespondPage(w, r, survey, shown, asSubmitted, problems, "", asParticipant)
		return
	}

	_, err = s.surveys.SubmitAnswers(r.Context(), survey.ID, version, &participant.ID,
		submission.Answers, durationFrom(r, s.clock.Now()), s.clock.Now())
	if errors.Is(err, store.ErrAlreadySubmitted) {
		render(w, r, http.StatusOK, templates.RespondAlreadySubmitted(survey.Title, s.latestStyle(r, survey.ID)))
		return
	}
	if err != nil {
		s.internalError(w, r, "submit participant response", err)
		return
	}
	s.recordCompletion(r, survey.ID, version.Questions, submission)
	render(w, r, http.StatusOK, templates.RespondThanks(
		thanksFor(survey, shown, answerSummary(r, shown.Questions, asSubmitted))))
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
		survey, err := s.publicSurvey(r, participant.SurveyID)
		if err == nil && survey.WorkspaceSuspended {
			s.respondSuspended(w, r)
			return fail()
		}
		title := ""
		var style domain.Style
		if err == nil {
			title = survey.Title
			style = s.latestStyle(r, survey.ID)
		}
		render(w, r, http.StatusOK, templates.RespondAlreadySubmitted(title, style))
		return fail()
	}

	survey, err := s.publicSurvey(r, participant.SurveyID)
	if errors.Is(err, store.ErrNotFound) {
		s.respondNotFound(w, r)
		return fail()
	}
	if err != nil {
		s.internalError(w, r, "load survey for participant", err)
		return fail()
	}
	// respondUnavailable draws a suspended workspace's survey as
	// Earful's plain page, before anything else about it.
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
