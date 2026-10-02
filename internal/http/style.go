package http

import (
	"net/http"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/web/templates"
)

// surveyStylePage is the Style tab: how a survey looks to the people
// answering it (ADR-0018). A save redirects here and names its outcome
// in the query, so the notice survives the redirect and a reload does
// not save again.
func (s *server) surveyStylePage(w http.ResponseWriter, r *http.Request) {
	notice := ""
	if r.URL.Query().Get("notice") == "saved" {
		notice = say(r, "style.notice.saved")
	}
	s.renderSurveyStyle(w, r, "", notice)
}

func (s *server) renderSurveyStyle(w http.ResponseWriter, r *http.Request, errMsg, notice string) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	status := http.StatusOK
	if errMsg != "" {
		status = http.StatusUnprocessableEntity
	}
	render(w, r, status, templates.SurveyStyle(info.Email, info.WorkspaceName, info.CSRFToken, templates.StyleData{
		Survey: viewSurvey(text(r), survey, s.clock.Now()),
		Theme:  draft.Style.ThemeName(),
		Error:  errMsg,
		Notice: notice,
	}))
}

// surveyStyleSave saves the style. It is a draft change like a question
// edit: it appends a revision, and respondents see it only once the next
// version is published (ADR-0001), so the page never changes its look
// under somebody who is answering it. A refused style leaves the draft
// as it was.
func (s *server) surveyStyleSave(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	style, err := domain.NewStyle(r.PostFormValue("theme"))
	if err == nil {
		err = draft.SetStyle(style)
	}
	if err != nil {
		if !isUserError(err) {
			s.internalError(w, r, "set style", err)
			return
		}
		s.renderSurveyStyle(w, r, sayErrorAlone(r, err), "")
		return
	}
	if err := s.surveys.SaveDraft(r.Context(), survey.ID, info.UserID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/style?notice=saved", http.StatusSeeOther)
}

// themeSheet draws every component of a respondent's page in one theme,
// for looking at a theme as a whole and for the gallery's axe scan
// (docs/style-guide.md, "Themes"). It is registered in development only:
// it is a tool for whoever is changing the stylesheet, and its sample
// survey is nobody's. The wording of the sample is here, as a real
// survey's is its creator's.
func (s *server) themeSheet(w http.ResponseWriter, r *http.Request) {
	style, err := domain.NewStyle(r.URL.Query().Get("theme"))
	if err != nil {
		style = domain.Style{}
	}
	three, yes := 3, true
	questions := []domain.Question{
		{IdentityID: "sheet-long", Type: domain.LongText, Text: "What should we keep doing?", Required: true},
		{IdentityID: "sheet-short", Type: domain.ShortText, Text: "Which session did you attend?", Required: true},
		{IdentityID: "sheet-single", Type: domain.SingleChoice, Text: "How did you hear of us?",
			Options: []string{"Email", "A friend", "Social media"}, AllowOther: true},
		{IdentityID: "sheet-multiple", Type: domain.MultipleChoice, Text: "Which topics would you come back for?",
			Options: []string{"Research", "Design", "Engineering"}},
		{IdentityID: "sheet-rating", Type: domain.RatingScale, Text: "How useful was the day?", ScaleMin: 1, ScaleMax: 5},
		{IdentityID: "sheet-yesno", Type: domain.YesNo, Text: "Would you come again?"},
		{IdentityID: "sheet-number", Type: domain.Number, Text: "How many people came with you?", ScaleMin: 0, ScaleMax: 12},
	}
	problem := domain.AnswerError{
		IdentityID: "sheet-short", Position: 2, Message: say(r, "answer.error.required"),
	}
	render(w, r, http.StatusOK, templates.ThemeSheet(templates.ThemeSheetData{
		Respond: templates.RespondData{
			Title:         "Theme sheet",
			WorkspaceName: "Corner Workshop",
			IsAnonymous:   true,
			Questions:     questions,
			Answers: map[string]domain.AnswerValue{
				"sheet-long":     {Text: "Shorter sessions, and more time to try things."},
				"sheet-single":   {Choice: "A friend"},
				"sheet-multiple": {Choices: []string{"Research", "Engineering"}},
				"sheet-rating":   {Number: &three},
				"sheet-yesno":    {Bool: &yes},
			},
			Errors:    map[string]string{problem.IdentityID: problem.Message},
			ErrorList: []domain.AnswerError{problem},
			Style:     style,
		},
		Answers: []templates.AnsweredQuestion{
			{Question: "What should we keep doing?", Answer: "Shorter sessions, and more time to try things."},
			{Question: "Which session did you attend?"},
		},
		Thanks: domain.ThankYou{
			Message:   "Thanks for coming.\nWe read every answer.",
			LinkLabel: "See the next dates",
			LinkURL:   "https://example.com/dates",
		},
		Focus: templates.ThemeSheetFocus{
			Question: "Controls with the focus ring showing",
			Field:    "A field",
			Option:   "An option",
		},
	}))
}
