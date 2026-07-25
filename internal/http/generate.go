package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/ws"
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
// than waiting for a whole JSON document to close.
func generateSystemPrompt() string {
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
		"Include \"scale_min\" and \"scale_max\" only for rating_scale (scale_min 0 or 1, scale_max 2–10). " +
		"nps is always 0–10 and needs neither.\n" +
		"Write neutral, specific, answerable questions in the language of the request. " +
		"Prefer a mix of types, and at most " + fmt.Sprint(maxGeneratedQuestions) + " questions."
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
		s.renderSurveyPage(w, r, "Describe what you want to ask about, and I'll draft some questions.", "")
		return
	}

	if err := s.aiMeter.Check(r.Context(), info.WorkspaceID); err != nil {
		s.renderSurveyPage(w, r, aiRefusalMessage(err), "")
		return
	}
	stream, err := s.ai.Generate(r.Context(), ai.GenerateRequest{
		System: generateSystemPrompt(),
		Prompt: prompt,
	})
	if err != nil {
		s.renderSurveyPage(w, r, aiRefusalMessage(err), "")
		return
	}
	counted := ai.Counted(stream)
	output, err := ai.Collect(counted)
	s.recordGeneration(r.Context(), info.WorkspaceID, survey.ID, prompt, counted.Chars())
	if err != nil && output == "" {
		s.logger.Error("question generation failed", "error", err)
		s.renderSurveyPage(w, r, "The model didn't answer. Try again in a moment.", "")
		return
	}

	added, skipped, err := s.appendGenerated(r.Context(), info.UserID, survey, output)
	if err != nil {
		s.internalError(w, r, "save generated questions", err)
		return
	}
	s.renderSurveyPage(w, r, "", generationNotice(added, skipped))
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
		_ = conn.Fail("empty", "Describe what you want to ask about first.")
		return
	}

	output, ok := s.streamGeneration(conn, info.WorkspaceID, survey.ID, prompt)
	if !ok {
		return
	}
	added, skipped, err := s.appendGenerated(conn.Context(), info.UserID, survey, output)
	if err != nil {
		s.logger.Error("saving generated questions failed", "error", err)
		_ = conn.Fail("save", "I drafted those but couldn't save them. Reload and try again.")
		return
	}
	_ = conn.Status(generationNotice(added, skipped))
	_ = conn.Done()
}

// streamGeneration runs the model call, relaying text to the creator as
// it arrives. The aiMeter.Check here is what
// TestAIProviderCallsAreMetered requires, and what the € breaker needs.
func (s *server) streamGeneration(conn *ws.Conn, workspaceID, surveyID uuid.UUID, prompt string) (string, bool) {
	ctx := conn.Context()
	if err := s.aiMeter.Check(ctx, workspaceID); err != nil {
		_ = conn.Fail("quota", aiRefusalMessage(err))
		return "", false
	}
	stream, err := s.ai.Generate(ctx, ai.GenerateRequest{
		System: generateSystemPrompt(),
		Prompt: prompt,
	})
	if err != nil {
		_ = conn.Fail("unavailable", aiRefusalMessage(err))
		return "", false
	}
	counted := ai.Counted(stream)
	defer counted.Close()
	defer s.recordGeneration(ctx, workspaceID, surveyID, prompt, counted.Chars())

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
				_ = conn.Fail("unavailable", "The model didn't answer. Try again in a moment.")
				return "", false
			}
			// Partial output is still worth keeping: whole lines parse.
			return output.String(), true
		}
	}
}

func (s *server) recordGeneration(ctx context.Context, workspaceID, surveyID uuid.UUID, prompt string, outChars int) {
	id := surveyID
	if err := s.aiMeter.Record(ctx, workspaceID, &id, string(ai.OpGenerate), outChars+len(prompt)); err != nil {
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
		if question.Type.NeedsScale() {
			question.ScaleMin, question.ScaleMax = raw.ScaleMin, raw.ScaleMax
		}
		questions = append(questions, question)
	}
	return questions
}

func generationNotice(added, skipped int) string {
	switch {
	case added == 0 && skipped == 0:
		return "The model didn't return any usable questions. Try describing your goal differently."
	case added == 0:
		return fmt.Sprintf("None of the %d drafted questions were usable. Try describing your goal differently.", skipped)
	case skipped == 0 && added == 1:
		return "Added 1 question to your draft — edit it like any other."
	case skipped == 0:
		return fmt.Sprintf("Added %d questions to your draft — edit them like any others.", added)
	default:
		return fmt.Sprintf("Added %d questions to your draft; %d were skipped because they weren't valid questions.", added, skipped)
	}
}

// aiRefusalMessage turns a metering or capability error into something a
// creator can act on. Quota and breaker are ordinary, temporary states,
// not failures, and must not read like a crash (stories 21, 67).
func aiRefusalMessage(err error) string {
	switch {
	case errors.Is(err, ai.ErrQuotaExceeded):
		return "This workspace has used its AI allowance for today. It resets tomorrow — everything else keeps working."
	case errors.Is(err, ai.ErrBreakerTripped):
		return "AI features are paused for today while we keep costs in check. They'll be back tomorrow."
	case errors.Is(err, ai.ErrUnsupported):
		return "AI isn't configured on this instance, so questions have to be written by hand."
	default:
		return "The AI service didn't respond. Try again in a moment."
	}
}

// canGenerate is what the editor needs to decide whether to offer the AI
// panel at all: an unconfigured capability is an absent feature, not a
// button that fails (Appendix D).
func (s *server) canGenerate() bool { return ai.Supports(s.ai, ai.OpGenerate) }
