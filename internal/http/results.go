package http

import (
	"errors"
	"github.com/TryEarful/earful/internal/uitext"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/audience"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/web/templates"
)

// Results (M7-T1). Answers are folded by Question Identity across every
// version, because that is what makes a survey improvable: rewording a
// question keeps its results comparable, and the results page says so
// rather than hiding it (ADR-0001, story 50).

func (s *server) surveyResults(w http.ResponseWriter, r *http.Request) {
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	results, err := s.surveys.SurveyResults(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "load results", err)
		return
	}
	notice := ""
	switch r.URL.Query().Get("notice") {
	case "response_deleted":
		notice = say(r, "results.notice.deleted")
	case "translated":
		notice = say(r, "results.notice.translated")
	}
	s.renderResults(w, r, survey, results, notice)
}

// renderResults is the one place the results page is built, so the
// insight run that redirects back here shows the same page a plain
// visit does.
func (s *server) renderResults(w http.ResponseWriter, r *http.Request,
	survey store.Survey, results store.Results, notice string) {
	info, _ := authFrom(r.Context())
	// A creator reading a global audience picks a language; translations
	// are cached per answer, and the original is always kept (M11-T2).
	lang := domain.NormalizeLang(r.URL.Query().Get("lang"))
	translations := map[uuid.UUID]store.AnswerTranslation{}
	if lang != "" {
		cached, err := s.surveys.AnswerTranslations(r.Context(), survey.ID, lang)
		if err != nil {
			s.internalError(w, r, "load answer translations", err)
			return
		}
		translations = cached
	}
	insight := templates.InsightView{Available: s.canAnalyze()}
	if run, err := s.surveys.LatestInsightRun(r.Context(), survey.ID); err == nil {
		insight = viewInsight(text(r), run, results, s.canAnalyze())
	}
	render(w, r, http.StatusOK, templates.SurveyResults(info.Email, info.WorkspaceName, info.CSRFToken,
		templates.SurveyResultsData{
			Survey:        viewSurvey(text(r), survey, s.clock.Now()),
			ResponseCount: len(results.Responses),
			Questions:     viewQuestionResults(text(r), results, translations),
			CanTranslate:  s.canTranslate(),
			TranslateLang: lang,
			TranslateName: languageName(text(r), lang),
			Insight:       insight,
			Notice:        notice,
			TableHeaders:  tableHeaders(text(r), results),
			Table:         viewResponseTable(text(r), results),
		}))
}

// viewQuestionResults turns stored answers into what a reader sees: a
// distribution for anything countable, the answers themselves for text.
// All formatting decisions live here, so the template holds no logic.
func viewQuestionResults(l uitext.Localizer, results store.Results, translations map[uuid.UUID]store.AnswerTranslation) []templates.QuestionResultsView {
	out := make([]templates.QuestionResultsView, 0, len(results.Questions))
	for _, question := range results.Questions {
		view := templates.QuestionResultsView{
			IdentityID:  question.IdentityID,
			Type:        question.Type,
			TypeLabel:   templates.Named(l, templates.QuestionTypeName(question.Type), string(question.Type)),
			Text:        question.Text,
			Answered:    len(question.Answers),
			SkippedNote: skippedNote(l, len(question.Answers), len(results.Responses)),
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
				text := templates.TextAnswerView{
					Text:          answer.Value.Text,
					VersionLabel:  "v" + strconv.Itoa(answer.VersionNumber),
					SubmittedAt:   l.DateTime(answer.SubmittedAt),
					Participant:   participantLabel(answer.ParticipantEmail),
					ResponseID:    answer.ResponseID.String(),
					AnswerLongish: len(answer.Value.Text) > 240,
				}
				if translated, ok := translations[answer.ID]; ok {
					text.Translation = translated.Text
					text.TranslationModel = modelLabel(l, translated.Model)
				}
				view.Texts = append(view.Texts, text)
			}
		case domain.SingleChoice, domain.MultipleChoice, domain.Dropdown:
			view.Distribution = choiceDistribution(l, question)
		case domain.YesNo:
			view.Distribution = yesNoDistribution(l, question)
		case domain.RatingScale, domain.NPS:
			view.Distribution = scaleDistribution(l, question)
			view.Summary = scaleSummary(l, question)
		case domain.Date:
			view.Distribution = dateDistribution(l, question)
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

func skippedNote(l uitext.Localizer, answered, responses int) string {
	skipped := responses - answered
	if skipped <= 0 {
		return ""
	}
	return l.N("results.skipped", skipped)
}

// choiceDistribution counts every option the question has ever offered,
// including options that only existed in an earlier version — dropping
// them would silently discard real answers.
func choiceDistribution(l uitext.Localizer, question store.QuestionResults) []templates.CountView {
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

	return toCountViews(l, labels, counts, total)
}

func yesNoDistribution(l uitext.Localizer, question store.QuestionResults) []templates.CountView {
	yes, no := l.T("answer.yes"), l.T("answer.no")
	counts := map[string]int{}
	total := 0
	for _, answer := range question.Answers {
		if answer.Value.Bool == nil {
			continue
		}
		if *answer.Value.Bool {
			counts[yes]++
		} else {
			counts[no]++
		}
		total++
	}
	return toCountViews(l, []string{yes, no}, counts, total)
}

// dateDistribution counts the answers given for each day, in calendar
// order. Only days somebody gave are listed: a range of every day in
// between would be mostly empty rows. Stored dates are yyyy-mm-dd, so
// sorting the text sorts the days.
func dateDistribution(l uitext.Localizer, question store.QuestionResults) []templates.CountView {
	perDay := map[string]int{}
	total := 0
	for _, answer := range question.Answers {
		if answer.Value.Date == "" {
			continue
		}
		perDay[answer.Value.Date]++
		total++
	}
	days := make([]string, 0, len(perDay))
	for day := range perDay {
		days = append(days, day)
	}
	sort.Strings(days)

	labels := make([]string, 0, len(days))
	counts := make(map[string]int, len(days))
	for _, day := range days {
		label := displayDate(l, day)
		labels = append(labels, label)
		counts[label] = perDay[day]
	}
	return toCountViews(l, labels, counts, total)
}

func scaleDistribution(l uitext.Localizer, question store.QuestionResults) []templates.CountView {
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
	return toCountViews(l, labels, counts, total)
}

// scaleSummary is the one-line read: an average, and for NPS the score
// itself, which is what anyone using NPS actually wants.
func scaleSummary(l uitext.Localizer, question store.QuestionResults) string {
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
		return l.T("results.summary.average", uitext.Args{"Average": l.Decimal(average, 1)})
	}
	score := (float64(promoters) - float64(detractors)) / float64(count) * 100
	return l.T("results.summary.nps", uitext.Args{
		"Score":      l.Signed(score, 0),
		"Average":    l.Decimal(average, 1),
		"Promoters":  promoters,
		"Detractors": detractors,
		"Passives":   count - promoters - detractors,
	})
}

func toCountViews(l uitext.Localizer, labels []string, counts map[string]int, total int) []templates.CountView {
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
			Share:   l.Percent(percent),
		})
	}
	return out
}

// tableHeaders and viewResponseTable are story 58's tabular view: the
// same shape as the CSV, so what a creator reads on screen and what they
// download agree.
func tableHeaders(l uitext.Localizer, results store.Results) []string {
	headers := []string{l.T("results.column.submitted"), l.T("results.column.version")}
	for _, question := range results.Questions {
		headers = append(headers, question.Text)
	}
	return headers
}

func viewResponseTable(l uitext.Localizer, results store.Results) []templates.ResponseRowView {
	out := make([]templates.ResponseRowView, 0, len(results.Responses))
	for _, response := range results.Responses {
		row := templates.ResponseRowView{
			ID:           response.ID.String(),
			SubmittedAt:  l.DateTime(response.SubmittedAt),
			VersionLabel: "v" + strconv.Itoa(response.VersionNumber),
			Participant:  participantLabel(response.ParticipantEmail),
		}
		for _, question := range results.Questions {
			row.Cells = append(row.Cells, truncateLabel(displayAnswer(l, response.Answers[question.IdentityID])))
		}
		out = append(out, row)
	}
	return out
}

// responseDelete removes one response from the results (M8-T1). It is a
// soft delete: results, stats and exports stop counting it immediately,
// and support can restore it until the purge job reaches it 30 days
// later. The creator is told both halves of that.
func (s *server) responseDelete(w http.ResponseWriter, r *http.Request) {
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	responseID, err := uuid.Parse(r.PathValue("responseID"))
	if err != nil {
		s.surveyNotFound(w, r)
		return
	}
	switch err := s.surveys.SoftDeleteResponse(r.Context(), survey.ID, responseID, s.clock.Now()); {
	case errors.Is(err, store.ErrNotFound):
		s.surveyNotFound(w, r)
		return
	case err != nil:
		s.internalError(w, r, "delete response", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/results?notice=response_deleted", http.StatusSeeOther)
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

// suppressedBuckets drops anything below the n < 5 threshold rather than
// rounding it or lumping it into "other" — an "other: 1" is just as
// revealing when the sample is small.
func suppressedBuckets(l uitext.Localizer, stats []store.SurveyStat) []templates.CountView {
	total := 0
	for _, stat := range stats {
		if !audience.Suppressed(stat.Count) {
			total += stat.Count
		}
	}
	var out []templates.CountView
	for _, stat := range stats {
		if audience.Suppressed(stat.Count) {
			continue
		}
		percent := 0
		if total > 0 {
			percent = int(float64(stat.Count)/float64(total)*100 + 0.5)
		}
		out = append(out, templates.CountView{
			Label: audienceGroup(l, stat.Metric, stat.Bucket), Count: stat.Count,
			Percent: percent, Share: l.Percent(percent),
		})
	}
	return out
}

func truncateLabel(text string) string {
	const limit = 48
	if len(text) <= limit {
		return text
	}
	return text[:limit-1] + "…"
}
