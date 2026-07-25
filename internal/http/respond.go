package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/antibot"
	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/store/db"
	"github.com/TryEarful/earful/web/templates"
)

// answerFieldPrefix namespaces answer fields so they cannot collide with
// the form's own fields (version_id, started_at, the honeypot).
const answerFieldPrefix = "q_"

// respondPage serves a survey to a respondent. The share link is the
// credential: there is no workspace scoping here, by design.
func (s *server) respondPage(w http.ResponseWriter, r *http.Request) {
	survey, version, ok := s.loadPublicSurvey(w, r)
	if !ok {
		return
	}
	// Counted here rather than in renderRespondPage: a validation
	// re-render is the same visit, not a second start (M7-T4).
	s.recordStart(r, survey.ID)
	s.renderRespondPage(w, r, survey, version, domain.Submission{Answers: map[string]domain.AnswerValue{}}, nil, "")
}

// renderRespondPage renders the respondent form in any state — fresh,
// re-rendered with a notice, or re-rendered with validation problems —
// always with a fresh form token and nonce, so the next submit is checked
// against this render. pc is nil on the anonymous path and carries the
// invite context on the participant path.
func (s *server) renderRespondPage(
	w http.ResponseWriter, r *http.Request,
	survey store.PublicSurvey, version store.ServedVersion,
	submission domain.Submission, problems []domain.AnswerError, notice string,
	pc ...*participantContext,
) {
	errs := make(map[string]string, len(problems))
	for _, p := range problems {
		errs[p.IdentityID] = p.Message
	}
	status := http.StatusOK
	if len(problems) > 0 {
		status = http.StatusUnprocessableEntity
	}
	data := templates.RespondData{
		SurveyID:        survey.ID.String(),
		Title:           survey.Title,
		WorkspaceName:   survey.WorkspaceName,
		IsAnonymous:     survey.IsAnonymous,
		VersionID:       version.ID.String(),
		Questions:       version.Questions,
		Answers:         submission.Answers,
		Errors:          errs,
		ErrorList:       problems,
		Notice:          notice,
		FormToken:       s.formTokens.Issue(survey.ID.String()),
		Nonce:           auth.NewToken(),
		AlreadyAnswered: s.hasAnsweredCookie(r, survey.ID),
	}
	if len(pc) > 0 && pc[0] != nil {
		data.ParticipantToken = pc[0].token
		data.ParticipantEmail = pc[0].email
		data.AlreadyAnswered = false // the one-per-token index owns this
	}
	// Voice is offered only when a transcriber is configured: an absent
	// capability is an absent feature, never a button that fails (M5-T1).
	if s.voiceEnabled() {
		if data.ParticipantToken != "" {
			data.VoicePath = "/p/" + data.ParticipantToken + "/voice"
		} else {
			data.VoicePath = "/s/" + survey.ID.String() + "/voice"
		}
		data.VoiceMaxSeconds = s.voiceAnswerSeconds()
	}
	render(w, r, status, templates.Respond(data))
}

// respondSubmit records a response, pinned to the version the respondent
// was actually served, after the anti-abuse gauntlet (M4-T5):
//
//	honeypot → form-token min-age → validation → challenge/rate limit →
//	double-click dedupe → write
//
// Rate limits deliberately sit after validation, so a respondent fixing
// required-field mistakes never burns limiter budget — only submissions
// that would actually be stored consume it.
func (s *server) respondSubmit(w http.ResponseWriter, r *http.Request) {
	surveyID, err := uuid.Parse(r.PathValue("surveyID"))
	if err != nil {
		s.respondNotFound(w, r)
		return
	}
	survey, err := s.surveys.PublicSurvey(r.Context(), surveyID)
	if errors.Is(err, store.ErrNotFound) {
		s.respondNotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, r, "load public survey", err)
		return
	}
	if !survey.IsAnonymous {
		// Invited surveys answer only through personal links (M4-T3).
		s.respondInviteOnly(w, r, survey)
		return
	}
	if !survey.State().AcceptsResponses(s.clock.Now()) {
		s.respondUnavailable(w, r, survey)
		return
	}

	// Honeypot: no person ever sees the field, so a value means
	// automation. The bot is shown success and nothing is written —
	// telling it what tripped would only train it.
	if r.PostFormValue("website") != "" {
		s.logAbuse(r, "honeypot")
		render(w, r, http.StatusOK, templates.RespondThanks(survey.Title, survey.IsAnonymous))
		return
	}

	// Pin to the version whose form was rendered, not to whatever is
	// newest now: publishing while someone is filling in the form must not
	// reattribute their answers (SPEC.md story 32).
	servedID, err := uuid.Parse(r.PostFormValue("version_id"))
	if err != nil {
		s.respondNotFound(w, r)
		return
	}
	version, err := s.surveys.ServedVersionByID(r.Context(), surveyID, servedID)
	if errors.Is(err, store.ErrNotFound) {
		// A version id that isn't this survey's: either tampering or a
		// very stale form. Re-serve the current version rather than
		// guessing what they meant.
		s.respondNotFound(w, r)
		return
	}
	if err != nil {
		s.internalError(w, r, "load served version", err)
		return
	}

	// The signed render timestamp gives the no-JS path its bot check: a
	// form answered faster than a person can read it is automation.
	switch err := s.formTokens.Check(surveyID.String(), r.PostFormValue("form_ts"), minFillTime); {
	case errors.Is(err, antibot.ErrFormTooFast):
		s.logAbuse(r, "too_fast")
		submission := parseSubmission(r, version.Questions)
		s.renderRespondPage(w, r, survey, version, submission, nil,
			"That was quick! Take a moment to review your answers, then submit again.")
		return
	case err != nil:
		s.logAbuse(r, "bad_form_token")
		submission := parseSubmission(r, version.Questions)
		s.renderRespondPage(w, r, survey, version, submission, nil,
			"This form had been open a while. Your answers are still here — review and submit again.")
		return
	}

	submission := parseSubmission(r, version.Questions)
	if problems := submission.Validate(version.Questions); len(problems) > 0 {
		s.renderRespondWithErrors(w, r, survey, version, submission, problems, false)
		return
	}

	// A solved proof-of-work buys the roomy rate bucket; skipping it
	// (no-JS clients, or bots) gets the tight one. A forged or replayed
	// solution is refused outright.
	limiterKey := s.clientIP(r) + "|" + surveyID.String()
	if payload := r.PostFormValue("altcha"); payload != "" {
		if err := s.challenges.Verify(payload); err != nil {
			s.logAbuse(r, "bad_challenge")
			render(w, r, http.StatusForbidden, templates.RespondUnavailable(survey.Title,
				"Couldn't verify your submission",
				"The anti-abuse check didn't pass. Reload the page and try again."))
			return
		}
		if !s.limitChallenged.Allow(limiterKey) {
			// Already throttled: the 429 is the whole response. Writing an
			// abuse_log row per rejected request would let a flood amplify
			// into unbounded DB writes — exactly what the limiter exists to
			// stop.
			s.respondRateLimited(w, r, survey)
			return
		}
	} else if !s.limitUnchallenged.Allow(limiterKey) {
		s.respondRateLimited(w, r, survey)
		return
	}

	// Double-click dedupe (M4-T2): the nonce identifies one form render;
	// a second submit of the same render is answered with the same thanks
	// page and writes nothing. Deliberate re-answering (a fresh form) is
	// allowed — the stated trade-off of anonymity.
	if nonce := r.PostFormValue("form_nonce"); nonce != "" && !s.submitNonces.FirstUse(nonce) {
		render(w, r, http.StatusOK, templates.RespondThanks(survey.Title, survey.IsAnonymous))
		return
	}

	if _, err := s.surveys.SubmitAnswers(r.Context(), surveyID, version, nil,
		submission.Answers, durationFrom(r, s.clock.Now()), s.clock.Now()); err != nil {
		s.internalError(w, r, "submit response", err)
		return
	}
	s.recordCompletion(r, surveyID, version.Questions, submission)
	s.setAnsweredCookie(w, surveyID)
	render(w, r, http.StatusOK, templates.RespondThanks(survey.Title, survey.IsAnonymous))
}

// minFillTime is the fastest plausible human submission: reading even one
// question and choosing an answer takes longer than this.
const minFillTime = 3 * time.Second

// respondChallenge mints an ALTCHA proof-of-work challenge for the
// enhancement script.
func (s *server) respondChallenge(w http.ResponseWriter, r *http.Request) {
	if !s.limitChallengeAPI.Allow(s.clientIP(r)) {
		// Already throttled — don't amplify a flood into abuse_log writes.
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	challenge, err := s.challenges.New()
	if err != nil {
		s.internalError(w, r, "mint challenge", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(challenge); err != nil {
		s.logger.Debug("encode challenge failed", "error", err)
	}
}

func (s *server) respondRateLimited(w http.ResponseWriter, r *http.Request, survey store.PublicSurvey) {
	render(w, r, http.StatusTooManyRequests, templates.RespondUnavailable(survey.Title,
		"Too many submissions",
		"This survey has received a lot of submissions from your network just now. Wait a while and try again."))
}

// setAnsweredCookie marks this browser as having answered, so a revisit
// shows a gentle "you already answered" note (soft dedupe, story 43).
// It is a courtesy, not a control: anonymous surveys accept repeat
// answers by design, and the cookie identifies nobody.
func (s *server) setAnsweredCookie(w http.ResponseWriter, surveyID uuid.UUID) {
	http.SetCookie(w, &http.Cookie{
		Name:     answeredCookieName(surveyID),
		Value:    "1",
		Path:     "/s/" + surveyID.String(),
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies(),
		SameSite: http.SameSiteLaxMode,
	})
}

func answeredCookieName(surveyID uuid.UUID) string {
	return "earful_answered_" + strings.ReplaceAll(surveyID.String(), "-", "")
}

func (s *server) hasAnsweredCookie(r *http.Request, surveyID uuid.UUID) bool {
	c, err := r.Cookie(answeredCookieName(surveyID))
	return err == nil && c.Value == "1"
}

// logAbuse records a rejected request in the quarantined abuse log. Best
// effort: an abuse-log failure must never take down the request path.
func (s *server) logAbuse(r *http.Request, kind string) {
	err := db.New(s.pool).AddAbuseEvent(r.Context(), db.AddAbuseEventParams{
		Ip:   s.clientIP(r),
		Path: r.URL.Path,
		Kind: kind,
	})
	if err != nil {
		s.logger.Error("abuse log write failed", "error", err)
	}
}

// previewPage renders the creator's draft through the real respondent
// renderer (M3-T6). Same template, same controls — the only differences
// are the banner and where the form posts.
func (s *server) previewPage(w http.ResponseWriter, r *http.Request) {
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	info, _ := authFrom(r.Context())
	render(w, r, http.StatusOK, templates.Respond(templates.RespondData{
		SurveyID:      survey.ID.String(),
		Title:         survey.Title,
		WorkspaceName: info.WorkspaceName,
		IsAnonymous:   survey.IsAnonymous,
		// A draft has no version; preview submissions are refused anyway.
		VersionID: "",
		Questions: draft.Questions,
		Answers:   map[string]domain.AnswerValue{},
		Errors:    map[string]string{},
		Preview:   true,
	}))
}

// previewSubmit exists so the preview form has somewhere honest to go. It
// writes nothing: preview cannot create a response because this handler is
// the only thing its form can reach, and it has no write path at all.
func (s *server) previewSubmit(w http.ResponseWriter, r *http.Request) {
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	render(w, r, http.StatusOK, templates.RespondPreviewSubmitted(survey.ID.String(), survey.Title))
}

// loadPublicSurvey resolves a share link to a survey and the version to
// serve, rendering the appropriate page and returning false when the
// survey cannot be answered.
func (s *server) loadPublicSurvey(w http.ResponseWriter, r *http.Request) (store.PublicSurvey, store.ServedVersion, bool) {
	surveyID, err := uuid.Parse(r.PathValue("surveyID"))
	if err != nil {
		s.respondNotFound(w, r)
		return store.PublicSurvey{}, store.ServedVersion{}, false
	}
	survey, err := s.surveys.PublicSurvey(r.Context(), surveyID)
	if errors.Is(err, store.ErrNotFound) {
		s.respondNotFound(w, r)
		return store.PublicSurvey{}, store.ServedVersion{}, false
	}
	if err != nil {
		s.internalError(w, r, "load public survey", err)
		return store.PublicSurvey{}, store.ServedVersion{}, false
	}
	if !survey.IsAnonymous {
		s.respondInviteOnly(w, r, survey)
		return store.PublicSurvey{}, store.ServedVersion{}, false
	}
	if !survey.State().AcceptsResponses(s.clock.Now()) {
		s.respondUnavailable(w, r, survey)
		return store.PublicSurvey{}, store.ServedVersion{}, false
	}
	version, err := s.surveys.LatestServedVersion(r.Context(), surveyID)
	if err != nil {
		s.internalError(w, r, "load served version", err)
		return store.PublicSurvey{}, store.ServedVersion{}, false
	}
	return survey, version, true
}

func (s *server) renderRespondWithErrors(
	w http.ResponseWriter, r *http.Request,
	survey store.PublicSurvey, version store.ServedVersion,
	submission domain.Submission, problems []domain.AnswerError, _ bool,
) {
	s.renderRespondPage(w, r, survey, version, submission, problems, "")
}

// respondUnavailable explains why a survey cannot be answered — closed,
// or never published — without leaking anything about the workspace.
func (s *server) respondUnavailable(w http.ResponseWriter, r *http.Request, survey store.PublicSurvey) {
	state := survey.State()
	switch state.StatusAt(s.clock.Now()) {
	case domain.StatusClosed:
		render(w, r, http.StatusGone, templates.RespondUnavailable(
			survey.Title,
			"This survey is closed",
			"It's no longer accepting responses. If you think it should still be open, contact whoever sent you the link."))
	default:
		// Never published: from outside, indistinguishable from a link
		// that was never valid.
		s.respondNotFound(w, r)
	}
}

// respondInviteOnly is what the public link shows for an invited survey:
// the survey exists, but answering goes through personal links only.
func (s *server) respondInviteOnly(w http.ResponseWriter, r *http.Request, survey store.PublicSurvey) {
	render(w, r, http.StatusForbidden, templates.RespondUnavailable(
		survey.Title,
		"This survey is invite-only",
		"Answers are collected through personal invitation links. If you were invited, use the link from your email; otherwise ask whoever runs the survey for an invitation."))
}

func (s *server) respondNotFound(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusNotFound, templates.RespondUnavailable(
		"",
		"Survey not found",
		"This link doesn't lead to a survey. Check that you copied all of it, or ask whoever sent it for a fresh link."))
}

// parseSubmission reads answers out of the form in the shape each
// question's type expects. Only fields matching a question in the served
// version are read, so extra fields are inert.
func parseSubmission(r *http.Request, questions []domain.Question) domain.Submission {
	answers := make(map[string]domain.AnswerValue, len(questions))
	for _, q := range questions {
		field := answerFieldPrefix + q.IdentityID
		var value domain.AnswerValue

		switch q.Type {
		case domain.LongText, domain.ShortText:
			value.Text = strings.TrimSpace(r.PostFormValue(field))
		case domain.SingleChoice, domain.Dropdown:
			value.Choice = r.PostFormValue(field)
		case domain.MultipleChoice:
			if r.PostForm != nil {
				for _, chosen := range r.PostForm[field] {
					if chosen != "" {
						value.Choices = append(value.Choices, chosen)
					}
				}
			}
		case domain.RatingScale, domain.NPS:
			if n, err := strconv.Atoi(r.PostFormValue(field)); err == nil {
				value.Number = &n
			}
		case domain.YesNo:
			switch r.PostFormValue(field) {
			case "yes":
				yes := true
				value.Bool = &yes
			case "no":
				no := false
				value.Bool = &no
			}
		}
		answers[q.IdentityID] = value
	}
	return domain.Submission{Answers: answers}
}

// durationFrom derives how long the response took from the timestamp the
// enhancement script stamped when the form was rendered. It is a
// per-response duration only (ADR-0009 blesses exactly this), and it is
// ignored unless it looks plausible — a clock-skewed or hand-edited value
// should not become a statistic.
func durationFrom(r *http.Request, now time.Time) *int {
	raw := r.PostFormValue("started_at")
	if raw == "" {
		return nil
	}
	millis, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	seconds := int(now.Sub(time.UnixMilli(millis)).Seconds())
	if seconds < 0 || seconds > int((6*time.Hour).Seconds()) {
		return nil
	}
	return &seconds
}
