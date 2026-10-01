package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/TryEarful/earful/internal/uitext"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/ws"
	"github.com/TryEarful/earful/web/templates"
)

// AI-drafted questions (M6-T3).
//
// Two entrances, one behaviour: a plain form post generates and appends
// synchronously, and a WebSocket does the same while showing the
// questions arriving. Both end with ordinary Draft content the creator
// edits, reorders or deletes like anything else (story 20) — generation
// is a starting point, never a special kind of question.
//
// One generation costs one model call either way. The socket does not
// preview-then-save: that would either charge twice or keep a pending
// result somewhere, and neither is worth it when the draft is already
// the editable, revisable, auditable place for work in progress.

// generateSystemPrompt asks for NDJSON — one question per line — so a
// reader can surface each question the moment its line completes, rather
// than waiting for a whole JSON document to close. withTitle also asks for
// a title line first, for a survey started from a description alone.
func generateSystemPrompt(withTitle bool) string {
	types := make([]string, 0, len(domain.QuestionTypes))
	for _, t := range domain.QuestionTypes {
		types = append(types, string(t))
	}
	return "You write survey questions. Reply with one JSON object per line and nothing else: " +
		"no prose, no numbering, no markdown fences.\n\n" +
		`Each line: {"type":"<type>","text":"<question>","required":<bool>,` +
		`"options":["…"],"scale_min":<int>,"scale_max":<int>}` + "\n\n" +
		"Allowed types: " + strings.Join(types, ", ") + ".\n" +
		"Include \"options\" only for single_choice, multiple_choice and dropdown (at least two, all distinct). " +
		"Include \"scale_min\" and \"scale_max\" only for rating_scale (scale_min 0 or 1, scale_max 2–10) and number. " +
		"nps is always 0–10 and needs neither.\n" +
		"date asks for a day on the calendar, such as when something happened, and needs neither.\n" +
		"number asks for a whole number, such as a count or an age; its scale_min and scale_max are the lowest and highest " +
		"sensible answers (scale_min below scale_max, both between -" + fmt.Sprint(domain.NumberBoundLimit) +
		" and " + fmt.Sprint(domain.NumberBoundLimit) + "). Use rating_scale, not number, for an opinion.\n" +
		"Write neutral, specific, answerable questions in the language of the request. " +
		"Prefer a mix of types, and at most " + fmt.Sprint(maxGeneratedQuestions) + " questions." +
		titleInstruction(withTitle)
}

// titleInstruction asks for the title as one more NDJSON line, so the
// questions keep their shape and parse, and a reply without it still
// yields its questions.
func titleInstruction(withTitle bool) string {
	if !withTitle {
		return ""
	}
	return "\n\nBefore the questions, write one line " + `{"title":"<title>"}` +
		": a short, plain title for the survey, under 80 characters, in the language of the request."
}

// maxGeneratedQuestions bounds one run. The draft itself caps at 100
// questions; this keeps a single enthusiastic prompt from filling it.
const maxGeneratedQuestions = 12

// surveyGenerate is the no-JS path: generate, append, redirect. The
// creator waits for the whole answer, which is the honest trade for not
// running any JavaScript.
func (s *server) surveyGenerate(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	prompt := strings.TrimSpace(r.PostFormValue("prompt"))
	if prompt == "" {
		s.renderSurveyPage(w, r, say(r, "generate.error.empty"), "")
		return
	}

	if err := s.aiMeter.Check(r.Context(), info.WorkspaceID); err != nil {
		s.renderSurveyPage(w, r, aiRefusalMessage(text(r), err), "")
		return
	}
	stream, err := s.ai.Generate(r.Context(), ai.GenerateRequest{
		System: generateSystemPrompt(false),
		Prompt: prompt,
	})
	if err != nil {
		s.renderSurveyPage(w, r, aiRefusalMessage(text(r), err), "")
		return
	}
	counted := ai.Counted(stream)
	output, err := ai.Collect(counted)
	s.recordGeneration(r.Context(), info.WorkspaceID, &survey.ID, prompt, counted.Chars())
	if err != nil && output == "" {
		s.logger.Error("question generation failed", "error", err)
		s.renderSurveyPage(w, r, say(r, "generate.error.silent"), "")
		return
	}

	added, skipped, err := s.appendGenerated(r.Context(), info.UserID, survey, output)
	if err != nil {
		s.internalError(w, r, "save generated questions", err)
		return
	}
	s.renderSurveyPage(w, r, "", generationNotice(text(r), added, skipped))
}

// surveyCreateFromPrompt starts a survey from a description: the same
// metered model call the editor's panel makes, then a survey whose draft
// holds what it produced, opened in the editor where every question is
// edited like any other.
//
// Nothing is created until the model has answered. A refusal (quota,
// breaker, provider) or a silent model hands the form back as typed, so
// the creator can try again or remove the description and create the
// survey by hand; an empty survey left behind by a failed run would be
// one more thing to find and delete. A run that answers but yields no
// usable question still creates the survey, as the creator asked, and
// the editor says so.
func (s *server) surveyCreateFromPrompt(w http.ResponseWriter, r *http.Request, form templates.NewSurveyData, closeAt *time.Time) {
	info, _ := authFrom(r.Context())
	ctx := r.Context()

	// A typed title is checked before the run, so a mistake in it does
	// not cost a generation.
	withTitle := strings.TrimSpace(form.Title) == ""
	if !withTitle {
		if err := domain.ValidateTitle(form.Title); err != nil {
			s.renderNewSurvey(w, r, form, sayError(r, err))
			return
		}
	}

	if err := s.aiMeter.Check(ctx, info.WorkspaceID); err != nil {
		s.renderNewSurvey(w, r, form, aiRefusalMessage(text(r), err))
		return
	}
	stream, err := s.ai.Generate(ctx, ai.GenerateRequest{
		System: generateSystemPrompt(withTitle),
		Prompt: form.Prompt,
	})
	if err != nil {
		s.renderNewSurvey(w, r, form, aiRefusalMessage(text(r), err))
		return
	}
	counted := ai.Counted(stream)
	output, err := ai.Collect(counted)
	if err != nil && output == "" {
		s.recordGeneration(ctx, info.WorkspaceID, nil, form.Prompt, counted.Chars())
		s.logger.Error("question generation failed", "error", err)
		s.renderNewSurvey(w, r, form, say(r, "generate.error.silent"))
		return
	}

	title := form.Title
	if withTitle {
		if title = parseGeneratedTitle(output); title == "" {
			title = say(r, "survey.new.default_title")
		}
	}
	survey, err := s.surveys.Create(ctx, info.WorkspaceID, info.UserID, title, form.Anonymous, closeAt)
	if err != nil {
		s.recordGeneration(ctx, info.WorkspaceID, nil, form.Prompt, counted.Chars())
		s.internalError(w, r, "create survey from a description", err)
		return
	}
	s.recordGeneration(ctx, info.WorkspaceID, &survey.ID, form.Prompt, counted.Chars())

	added, skipped, err := s.appendGenerated(ctx, info.UserID, survey, output)
	if err != nil {
		s.internalError(w, r, "save generated questions", err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/surveys/%s?added=%d&skipped=%d", survey.ID, added, skipped), http.StatusSeeOther)
}

// surveyGenerateSocket is the same operation with the questions visible
// as they arrive. It runs behind requireAuth, and ws.Accept refuses a
// cross-origin handshake (the stdlib cross-origin layer never sees a GET).
func (s *server) surveyGenerateSocket(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	conn, err := ws.Accept(w, r, ws.Options{})
	if err != nil {
		s.logger.Debug("generate socket not accepted", "error", err)
		return
	}
	defer conn.Close()

	msg, err := conn.Receive()
	if err != nil || msg.Control.Action != "generate" {
		return
	}
	prompt := strings.TrimSpace(msg.Control.Param("prompt"))
	if prompt == "" {
		_ = conn.Fail("empty", say(r, "generate.error.empty_first"))
		return
	}

	output, ok := s.streamGeneration(conn, text(r), info.WorkspaceID, survey.ID, prompt)
	if !ok {
		return
	}
	added, skipped, err := s.appendGenerated(conn.Context(), info.UserID, survey, output)
	if err != nil {
		s.logger.Error("saving generated questions failed", "error", err)
		_ = conn.Fail("save", say(r, "generate.error.save"))
		return
	}
	_ = conn.Status(generationNotice(text(r), added, skipped))
	_ = conn.Done()
}

// streamGeneration runs the model call, relaying text to the creator as
// it arrives. The aiMeter.Check here is what
// TestAIProviderCallsAreMetered requires, and what the € breaker needs.
func (s *server) streamGeneration(conn *ws.Conn, l uitext.Localizer, workspaceID, surveyID uuid.UUID, prompt string) (string, bool) {
	ctx := conn.Context()
	if err := s.aiMeter.Check(ctx, workspaceID); err != nil {
		_ = conn.Fail("quota", aiRefusalMessage(l, err))
		return "", false
	}
	stream, err := s.ai.Generate(ctx, ai.GenerateRequest{
		System: generateSystemPrompt(false),
		Prompt: prompt,
	})
	if err != nil {
		_ = conn.Fail("unavailable", aiRefusalMessage(l, err))
		return "", false
	}
	counted := ai.Counted(stream)
	defer counted.Close()
	defer s.recordGeneration(ctx, workspaceID, &surveyID, prompt, counted.Chars())

	var output strings.Builder
	for {
		fragment, err := counted.Recv()
		if fragment != "" {
			output.WriteString(fragment)
			if sendErr := conn.Chunk(fragment); sendErr != nil {
				return "", false // the creator navigated away; stop spending
			}
		}
		if err != nil {
			if isStreamEnd(err) {
				return output.String(), true
			}
			s.logger.Error("generation stream failed", "error", err)
			if output.Len() == 0 {
				_ = conn.Fail("unavailable", l.T("generate.error.silent"))
				return "", false
			}
			// Partial output is still worth keeping: whole lines parse.
			return output.String(), true
		}
	}
}

// recordGeneration charges a run to the workspace. surveyID is nil when
// the run produced no survey to attribute it to; it is charged anyway.
func (s *server) recordGeneration(ctx context.Context, workspaceID uuid.UUID, surveyID *uuid.UUID, prompt string, outChars int) {
	if err := s.aiMeter.Record(ctx, workspaceID, surveyID, string(ai.OpGenerate), outChars+len(prompt)); err != nil {
		s.logger.Error("recording generation usage failed", "error", err)
	}
}

// appendGenerated parses the model's lines and adds every valid question
// to the draft, in one save — so the audit log shows one entry and the
// creator gets one undo point, not twelve.
func (s *server) appendGenerated(ctx context.Context, userID uuid.UUID, survey store.Survey, output string) (added, skipped int, err error) {
	draft, _, err := s.surveys.Draft(ctx, survey.ID)
	if err != nil {
		return 0, 0, err
	}

	for _, question := range parseGeneratedQuestions(output) {
		if added >= maxGeneratedQuestions {
			skipped++
			continue
		}
		question.IdentityID = uuid.NewString() // a new question, a new identity
		if addErr := draft.Add(question); addErr != nil {
			// A question the editor itself would refuse is dropped, and
			// counted, rather than quietly accepted (story 20).
			skipped++
			continue
		}
		added++
	}
	if added == 0 {
		return 0, skipped, nil
	}
	if err := s.surveys.SaveDraft(ctx, survey.ID, userID, draft, s.clock.Now()); err != nil {
		return 0, 0, err
	}
	return added, skipped, nil
}

// parseGeneratedQuestions reads NDJSON leniently: models add fences,
// stray prose and trailing commentary, and one bad line must not cost
// the creator the other eleven.
func parseGeneratedQuestions(output string) []domain.Question {
	var questions []domain.Question
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, ",")
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			continue
		}
		var raw struct {
			Title    string   `json:"title"`
			Type     string   `json:"type"`
			Text     string   `json:"text"`
			Options  []string `json:"options"`
			Required bool     `json:"required"`
			ScaleMin int      `json:"scale_min"`
			ScaleMax int      `json:"scale_max"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if raw.Type == "" && raw.Title != "" {
			continue // the title line is not a question, and not a skipped one
		}
		question := domain.Question{
			Type:     domain.QuestionType(strings.TrimSpace(raw.Type)),
			Text:     strings.TrimSpace(raw.Text),
			Required: raw.Required,
		}
		// Models supply fields the type does not use — a yes/no question
		// with ["Yes","No"] options, a scale on an NPS question. Keeping
		// them would put dead data in the draft, so each type takes only
		// what it means.
		if question.Type.NeedsOptions() {
			question.Options = raw.Options
		}
		if question.Type.HasBounds() {
			question.ScaleMin, question.ScaleMax = raw.ScaleMin, raw.ScaleMax
		}
		questions = append(questions, question)
	}
	return questions
}

// parseGeneratedTitle returns the title line's title, or "" when the
// reply has none a survey could carry; the caller then names the survey
// itself rather than refusing a run that produced questions.
func parseGeneratedTitle(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSuffix(strings.TrimSpace(line), ",")
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			continue
		}
		var raw struct {
			Title string `json:"title"`
			Type  string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil || raw.Type != "" || raw.Title == "" {
			continue
		}
		title := strings.Join(strings.Fields(raw.Title), " ")
		if domain.ValidateTitle(title) != nil {
			return ""
		}
		return title
	}
	return ""
}

func generationNotice(l uitext.Localizer, added, skipped int) string {
	switch {
	case added == 0 && skipped == 0:
		return l.T("generate.notice.nothing")
	case added == 0:
		return l.N("generate.notice.unusable", skipped)
	case skipped == 0:
		return l.N("generate.notice.added", added)
	default:
		return l.T("generate.notice.added_some", uitext.Args{"Added": added, "Skipped": skipped})
	}
}

// aiRefusalMessage turns a metering or capability error into something a
// creator can act on. Quota and breaker are ordinary, temporary states,
// not failures, and must not read like a crash (stories 21, 67).
func aiRefusalMessage(l uitext.Localizer, err error) string {
	switch {
	case errors.Is(err, ai.ErrQuotaExceeded):
		return l.T("ai.refused.quota")
	case errors.Is(err, ai.ErrBreakerTripped):
		return l.T("ai.refused.paused")
	case errors.Is(err, ai.ErrUnsupported):
		return l.T("ai.refused.absent")
	default:
		return l.T("ai.refused.silent")
	}
}

// unnamedModel is what is stored as the model of a summary or a
// translation when the operator named none. It is a value, kept with
// the run and written into exports, and modelLabel is what words it.
const unnamedModel = "an unnamed model"

// modelLabel is the name of a model as a reader is told it.
func modelLabel(l uitext.Localizer, stored string) string {
	if stored == unnamedModel {
		return l.T("ai.model.unnamed")
	}
	return stored
}

// canGenerate is what the editor needs to decide whether to offer the AI
// panel at all: an unconfigured capability is an absent feature, not a
// button that fails (Appendix D).
func (s *server) canGenerate() bool { return ai.Supports(s.ai, ai.OpGenerate) }
