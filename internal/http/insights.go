package http

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/ws"
	"github.com/TryEarful/earful/web/templates"
)

// Insight Summaries (M10). Hundreds of answers become a story in
// minutes — with three rules that make that trustworthy rather than
// merely fast:
//
//   - The prompt never contains participant identity. Who said something
//     is irrelevant to what was said, and sending it would put a
//     respondent's name into a model for no reason. A test greps the
//     recorded prompt for the seeded addresses.
//   - Output is stored append-only and always displayed with the model
//     and the time it ran (story 53). Analysis must never be able to
//     pass for data.
//   - A re-run with no new responses serves the stored run and does not
//     call the model at all (story 54). Curiosity should not cost money.

// insightsSystemPrompt frames the task. It is deliberately strict about
// invention: a summary that makes up a number is worse than no summary.
const insightsSystemPrompt = `You are a research analyst reading survey responses.

Write a short report with these sections:
- Themes: the three to five ideas that recur, each with roughly how common it is.
- Patterns: anything that differs between groups of answers, or changed over time.
- Representative quotes: three to six short verbatim quotes, each on its own line, that
  show what respondents actually said. Quote exactly; never paraphrase inside quotation marks.
- What to look at next: two or three specific suggestions for the survey's owner.

Rules:
- Use only what is in the responses. Never invent a number, a quote or a trend.
- Say when the sample is too small to support a conclusion.
- Plain text, no markdown headings, no bullet characters other than "- ".
- Write in the language most of the responses are written in.`

// insightPromptLimits bound one run's cost. A survey with thousands of
// answers is summarised from a large sample rather than all of it, and
// the report says so.
const (
	maxInsightAnswers    = 300
	maxInsightAnswerLen  = 600
	insightSampleWarning = "\n\n(Only the most recent %d answers to each question were included.)"
)

// surveyInsights is the no-JS path: run (or serve the cache) and render.
func (s *server) surveyInsights(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	results, err := s.surveys.SurveyResults(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "load results for insights", err)
		return
	}
	if len(results.Responses) == 0 {
		http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/results", http.StatusSeeOther)
		return
	}

	// Already current: show the stored run rather than paying for the
	// same reading twice (story 54).
	if _, cached := s.cachedInsight(r.Context(), survey.ID, results); cached {
		http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/results", http.StatusSeeOther)
		return
	}

	if err := s.aiMeter.Check(r.Context(), info.WorkspaceID); err != nil {
		s.renderResults(w, r, survey, results, aiRefusalMessage(err))
		return
	}
	prompt := insightPrompt(survey.Title, results)
	stream, err := s.ai.Analyze(r.Context(), ai.AnalyzeRequest{
		System: insightsSystemPrompt,
		Prompt: prompt,
	})
	if err != nil {
		s.renderResults(w, r, survey, results, aiRefusalMessage(err))
		return
	}
	counted := ai.Counted(stream)
	output, err := ai.Collect(counted)
	s.recordInsightUsage(r.Context(), info.WorkspaceID, survey.ID, prompt, counted.Chars())
	if err != nil && output == "" {
		s.logger.Error("insight run failed", "error", err)
		s.renderResults(w, r, survey, results,
			"The analysis didn't complete. Try again in a moment.")
		return
	}
	if _, err := s.storeInsight(r.Context(), survey.ID, results, output); err != nil {
		s.internalError(w, r, "store insight run", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/results", http.StatusSeeOther)
}

// surveyInsightsSocket streams the same run as it is written, which for
// a page of prose is the difference between "working…" and reading.
func (s *server) surveyInsightsSocket(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	conn, err := ws.Accept(w, r, ws.Options{})
	if err != nil {
		s.logger.Debug("insights socket not accepted", "error", err)
		return
	}
	defer conn.Close()

	if msg, err := conn.Receive(); err != nil || msg.Control.Action != "analyze" {
		return
	}
	ctx := conn.Context()

	results, err := s.surveys.SurveyResults(ctx, survey.ID)
	if err != nil {
		_ = conn.Fail("unavailable", "Couldn't read the responses just now.")
		return
	}
	if len(results.Responses) == 0 {
		_ = conn.Fail("empty", "There are no responses to analyse yet.")
		return
	}
	// Cached: send the stored run and spend nothing.
	if run, ok := s.cachedInsight(ctx, survey.ID, results); ok {
		_ = conn.Chunk(run.Output)
		_ = conn.Status("cached")
		_ = conn.Done()
		return
	}

	if err := s.aiMeter.Check(ctx, info.WorkspaceID); err != nil {
		_ = conn.Fail("quota", aiRefusalMessage(err))
		return
	}
	prompt := insightPrompt(survey.Title, results)
	stream, err := s.ai.Analyze(ctx, ai.AnalyzeRequest{System: insightsSystemPrompt, Prompt: prompt})
	if err != nil {
		_ = conn.Fail("unavailable", aiRefusalMessage(err))
		return
	}
	counted := ai.Counted(stream)
	defer counted.Close()
	defer s.recordInsightUsage(ctx, info.WorkspaceID, survey.ID, prompt, counted.Chars())

	var output strings.Builder
	for {
		fragment, recvErr := counted.Recv()
		if fragment != "" {
			output.WriteString(fragment)
			if err := conn.Chunk(fragment); err != nil {
				return // reader gone; stop spending
			}
		}
		if recvErr != nil {
			if !isStreamEnd(recvErr) {
				s.logger.Error("insight stream failed", "error", recvErr)
				if output.Len() == 0 {
					_ = conn.Fail("unavailable", "The analysis didn't complete. Try again in a moment.")
					return
				}
			}
			break
		}
	}
	if _, err := s.storeInsight(ctx, survey.ID, results, output.String()); err != nil {
		s.logger.Error("storing insight run failed", "error", err)
	}
	_ = conn.Done()
}

// cachedInsight returns the newest run when it still reflects the
// current responses.
func (s *server) cachedInsight(ctx context.Context, surveyID uuid.UUID, results store.Results) (store.InsightRun, bool) {
	run, err := s.surveys.LatestInsightRun(ctx, surveyID)
	if err != nil {
		return store.InsightRun{}, false
	}
	watermark, count := results.Watermark()
	return run, run.Fresh(watermark, count)
}

func (s *server) storeInsight(ctx context.Context, surveyID uuid.UUID, results store.Results, output string) (store.InsightRun, error) {
	watermark, count := results.Watermark()
	return s.surveys.CreateInsightRun(ctx, store.InsightRun{
		SurveyID:      surveyID,
		Watermark:     watermark,
		ResponseCount: count,
		Model:         s.analyzeModelName(),
		Output:        strings.TrimSpace(output),
	}, s.clock.Now())
}

func (s *server) recordInsightUsage(ctx context.Context, workspaceID, surveyID uuid.UUID, prompt string, outChars int) {
	id := surveyID
	if err := s.aiMeter.Record(ctx, workspaceID, &id, string(ai.OpAnalyze), outChars+len(prompt)); err != nil {
		s.logger.Error("recording insight usage failed", "error", err)
	}
}

// analyzeModelName is what the label says. Configuration, never a
// hard-coded name — and when the operator named no model, the label says
// so rather than inventing one.
func (s *server) analyzeModelName() string {
	for _, candidate := range []string{s.cfg.AIModelAnalyze, s.cfg.AIModel, s.cfg.AIProvider} {
		if candidate != "" && candidate != "none" {
			return candidate
		}
	}
	return "an unnamed model"
}

func (s *server) canAnalyze() bool { return ai.Supports(s.ai, ai.OpAnalyze) }

// insightPrompt renders the responses for the model: every question with
// its current wording, a distribution where one exists, and the answers
// themselves for text questions.
//
// What is deliberately not in here: participant emails, response ids,
// durations, countries, browsers. None of it would improve the reading,
// and all of it would be a respondent's identity in someone else's
// model.
func insightPrompt(title string, results store.Results) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Survey: %s\nResponses: %d\n", title, len(results.Responses))

	truncated := false
	for i, question := range results.Questions {
		fmt.Fprintf(&b, "\n--- Question %d (%s) ---\n%s\n", i+1, question.Type, question.Text)
		if question.Reworded() {
			b.WriteString("Earlier wordings of the same question:\n")
			for _, wording := range question.Wordings[:len(question.Wordings)-1] {
				fmt.Fprintf(&b, "- v%d: %s\n", wording.VersionNumber, wording.Text)
			}
		}

		switch question.Type {
		case domain.LongText, domain.ShortText:
			answers := question.Answers
			if len(answers) > maxInsightAnswers {
				answers = answers[len(answers)-maxInsightAnswers:]
				truncated = true
			}
			fmt.Fprintf(&b, "Answers (%d of %d):\n", len(answers), len(question.Answers))
			for _, answer := range answers {
				text := strings.TrimSpace(answer.Value.Text)
				if text == "" {
					continue
				}
				if len(text) > maxInsightAnswerLen {
					text = text[:maxInsightAnswerLen] + "…"
					truncated = true
				}
				fmt.Fprintf(&b, "- %s\n", strings.ReplaceAll(text, "\n", " "))
			}
		default:
			counts := map[string]int{}
			for _, answer := range question.Answers {
				counts[answer.Value.Display()]++
			}
			b.WriteString("Counts:\n")
			for _, label := range sortedKeys(counts) {
				fmt.Fprintf(&b, "- %s: %d\n", label, counts[label])
			}
		}
	}
	if truncated {
		fmt.Fprintf(&b, insightSampleWarning, maxInsightAnswers)
	}
	return b.String()
}

func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	// Highest count first, then alphabetically, so the prompt reads the
	// way a person would summarise it.
	sortStrings(keys, func(a, b string) bool {
		if counts[a] != counts[b] {
			return counts[a] > counts[b]
		}
		return a < b
	})
	return keys
}

func sortStrings(items []string, less func(a, b string) bool) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && less(items[j], items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// viewInsight formats a stored run for the results page: the summary,
// and the label that keeps it from passing for data (story 53).
func viewInsight(run store.InsightRun, results store.Results, available bool) templates.InsightView {
	view := templates.InsightView{Available: available}
	if run.Output == "" {
		return view
	}
	watermark, count := results.Watermark()
	view.Present = true
	view.Output = run.Output
	view.Model = run.Model
	view.GeneratedAt = run.CreatedAt.Format(dateTimeLayout)
	view.ResponseCount = run.ResponseCount
	view.Stale = !run.Fresh(watermark, count)
	if view.Stale {
		view.StaleNote = fmt.Sprintf("%d responses have arrived since this was written.",
			count-run.ResponseCount)
		if count-run.ResponseCount <= 0 {
			view.StaleNote = "The responses have changed since this was written."
		}
	}
	view.CountLabel = strconv.Itoa(run.ResponseCount) + " responses"
	return view
}
