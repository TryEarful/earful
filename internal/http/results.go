package http

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/web/templates"
)

// Results (M7-T1). Answers are folded by Question Identity across every
// version, because that is what makes a survey improvable: rewording a
// question keeps its results comparable, and the results page says so
// rather than hiding it (ADR-0001, story 50).

func (s *server) surveyResults(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	results, err := s.surveys.SurveyResults(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "load results", err)
		return
	}
	render(w, r, http.StatusOK, templates.SurveyResults(info.Email, info.WorkspaceName, info.CSRFToken,
		templates.SurveyResultsData{
			Survey:        viewSurvey(survey, s.clock.Now()),
			ResponseCount: len(results.Responses),
			Questions:     viewQuestionResults(results),
		}))
}

// viewQuestionResults turns stored answers into what a reader sees: a
// distribution for anything countable, the answers themselves for text.
// All formatting decisions live here, so the template holds no logic.
func viewQuestionResults(results store.Results) []templates.QuestionResultsView {
	out := make([]templates.QuestionResultsView, 0, len(results.Questions))
	for _, question := range results.Questions {
		view := templates.QuestionResultsView{
			IdentityID:  question.IdentityID,
			Type:        question.Type,
			TypeLabel:   question.Type.Label(),
			Text:        question.Text,
			Answered:    len(question.Answers),
			SkippedNote: skippedNote(len(question.Answers), len(results.Responses)),
		}
		if question.Reworded() {
			for _, wording := range question.Wordings {
				view.Wordings = append(view.Wordings, templates.WordingView{
					Label: "v" + strconv.Itoa(wording.VersionNumber),
					Text:  wording.Text,
				})
			}
		}

		switch question.Type {
		case domain.LongText, domain.ShortText:
			for _, answer := range question.Answers {
				view.Texts = append(view.Texts, templates.TextAnswerView{
					Text:          answer.Value.Text,
					VersionLabel:  "v" + strconv.Itoa(answer.VersionNumber),
					SubmittedAt:   answer.SubmittedAt.Format(dateTimeLayout),
					Participant:   participantLabel(answer.ParticipantEmail),
					ResponseID:    answer.ResponseID.String(),
					AnswerLongish: len(answer.Value.Text) > 240,
				})
			}
		case domain.SingleChoice, domain.MultipleChoice, domain.Dropdown:
			view.Distribution = choiceDistribution(question)
		case domain.YesNo:
			view.Distribution = yesNoDistribution(question)
		case domain.RatingScale, domain.NPS:
			view.Distribution = scaleDistribution(question)
			view.Summary = scaleSummary(question)
		}
		out = append(out, view)
	}
	return out
}

func participantLabel(email *string) string {
	if email == nil {
		return ""
	}
	return *email
}

func skippedNote(answered, responses int) string {
	skipped := responses - answered
	if skipped <= 0 {
		return ""
	}
	if skipped == 1 {
		return "1 respondent skipped this"
	}
	return fmt.Sprintf("%d respondents skipped this", skipped)
}

// choiceDistribution counts every option the question has ever offered,
// including options that only existed in an earlier version — dropping
// them would silently discard real answers.
func choiceDistribution(question store.QuestionResults) []templates.CountView {
	counts := map[string]int{}
	total := 0
	for _, answer := range question.Answers {
		switch {
		case answer.Value.Choice != "":
			counts[answer.Value.Choice]++
			total++
		case len(answer.Value.Choices) > 0:
			for _, choice := range answer.Value.Choices {
				counts[choice]++
			}
			total++
		}
	}
	labels := append([]string(nil), question.Options...)
	seen := map[string]bool{}
	for _, label := range labels {
		seen[label] = true
	}
	var extra []string
	for label := range counts {
		if !seen[label] {
			extra = append(extra, label)
		}
	}
	sort.Strings(extra)
	labels = append(labels, extra...)

	return toCountViews(labels, counts, total)
}

func yesNoDistribution(question store.QuestionResults) []templates.CountView {
	counts := map[string]int{}
	total := 0
	for _, answer := range question.Answers {
		if answer.Value.Bool == nil {
			continue
		}
		if *answer.Value.Bool {
			counts["Yes"]++
		} else {
			counts["No"]++
		}
		total++
	}
	return toCountViews([]string{"Yes", "No"}, counts, total)
}

func scaleDistribution(question store.QuestionResults) []templates.CountView {
	counts := map[string]int{}
	total := 0
	for _, answer := range question.Answers {
		if answer.Value.Number == nil {
			continue
		}
		counts[strconv.Itoa(*answer.Value.Number)]++
		total++
	}
	var labels []string
	for _, point := range question.AsQuestion().ScalePoints() {
		labels = append(labels, strconv.Itoa(point))
	}
	return toCountViews(labels, counts, total)
}

// scaleSummary is the one-line read: an average, and for NPS the score
// itself, which is what anyone using NPS actually wants.
func scaleSummary(question store.QuestionResults) string {
	var sum, count, promoters, detractors int
	for _, answer := range question.Answers {
		if answer.Value.Number == nil {
			continue
		}
		value := *answer.Value.Number
		sum += value
		count++
		switch {
		case value >= 9:
			promoters++
		case value <= 6:
			detractors++
		}
	}
	if count == 0 {
		return ""
	}
	average := float64(sum) / float64(count)
	if question.Type != domain.NPS {
		return fmt.Sprintf("Average %.1f", average)
	}
	score := (float64(promoters) - float64(detractors)) / float64(count) * 100
	return fmt.Sprintf("NPS %+.0f · average %.1f · %d promoters, %d detractors, %d passives",
		score, average, promoters, detractors, count-promoters-detractors)
}

func toCountViews(labels []string, counts map[string]int, total int) []templates.CountView {
	out := make([]templates.CountView, 0, len(labels))
	for _, label := range labels {
		count := counts[label]
		percent := 0
		if total > 0 {
			percent = int(float64(count)/float64(total)*100 + 0.5)
		}
		out = append(out, templates.CountView{
			Label:   label,
			Count:   count,
			Percent: percent,
			Share:   strconv.Itoa(percent) + "%",
		})
	}
	return out
}

// csvSafe defuses spreadsheet formula injection: a cell a spreadsheet
// would evaluate is prefixed with an apostrophe, which Excel and Sheets
// both read as "this is text". Tabs and carriage returns lead the same
// way in some importers, so they count too (M7-T2's AC).
func csvSafe(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}

// resultsCSV streams one row per response, one column per Question
// Identity. Columns are headed with the current wording; the version
// column is what makes an older row's different wording traceable.
func (s *server) resultsCSV(w http.ResponseWriter, r *http.Request) {
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	results, err := s.surveys.SurveyResults(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "load results", err)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+csvFilename(survey.Title)+`"`)
	if err := writeResultsCSV(w, survey, results); err != nil {
		// Headers are already out; all that is left is to say so in the log.
		s.logger.Error("writing results csv failed", "error", err)
	}
}

func csvFilename(title string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		case r == ' ':
			return '-'
		default:
			return -1
		}
	}, title)
	if safe == "" {
		safe = "survey"
	}
	return strings.ToLower(safe) + "-responses.csv"
}
