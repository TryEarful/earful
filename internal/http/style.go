package http

import (
	"errors"
	"net/http"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/uitext"
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
	s.renderSurveyStyle(w, r, nil, nil, notice)
}

// renderSurveyStyle draws the Style tab. typed is what the creator
// entered when the save was refused, shown again in place of the draft's
// style so that nothing has to be typed twice, and problem is why.
func (s *server) renderSurveyStyle(w http.ResponseWriter, r *http.Request, typed *domain.Style, problem error, notice string) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	data := templates.StyleData{
		Survey: viewSurvey(text(r), survey, s.clock.Now()),
		Style:  draft.Style,
		Notice: notice,
	}
	status := http.StatusOK
	if problem != nil {
		status = http.StatusUnprocessableEntity
		if typed != nil {
			data.Style = *typed
		}
		data.Error = sayErrorAlone(r, problem)
		var where domain.StyleError
		if errors.As(problem, &where) && where.Part != domain.StyleTheme {
			data.ErrorPart, data.ErrorLink = where.Part, where.Link
			data.Error = say(r, styleErrorPlace(where), uitext.Args{"Position": where.Link, "Problem": sayError(r, where.Err)})
			data.FieldError = sayErrorAlone(r, where.Err)
		}
	}
	render(w, r, status, templates.SurveyStyle(info.Email, info.WorkspaceName, info.CSRFToken, data))
}

// styleErrorPlace is the message that says where on the Style tab a
// problem is, around the problem itself: the summary at the head of the
// form names the field, since the field may be a screen away.
func styleErrorPlace(e domain.StyleError) uitext.ID {
	switch e.Part {
	case domain.StyleHeaderName:
		return "style.error.place.name"
	case domain.StyleHeaderText:
		return "style.error.place.tagline"
	case domain.StyleHeaderLinks:
		if e.Link > 0 {
			return "style.error.place.header_link"
		}
		return "style.error.place.header_links"
	case domain.StyleFooterText:
		return "style.error.place.footer_text"
	case domain.StyleFooterLinks:
		if e.Link > 0 {
			return "style.error.place.footer_link"
		}
	}
	return "style.error.place.footer_links"
}

// surveyStyleSave saves the style. It is a draft change like a question
// edit: it appends a revision, and respondents see it only once the next
// version is published (ADR-0001), so the page never changes its look
// under somebody who is answering it. A refused style leaves the draft
// as it was.
//
// The form's fields are laid over the draft's style, part by part, so
// that a part of the style this form does not carry is kept.
func (s *server) surveyStyleSave(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	style, err := styleFromForm(r, draft.Style)
	if err == nil {
		err = draft.SetStyle(style)
	}
	if err != nil {
		if !isUserError(err) {
			s.internalError(w, r, "set style", err)
			return
		}
		s.renderSurveyStyle(w, r, &style, err, "")
		return
	}
	if err := s.surveys.SaveDraft(r.Context(), survey.ID, info.UserID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/style?notice=saved", http.StatusSeeOther)
}

// styleFromForm lays the Style tab's form over the style it edits. A
// part is read only where the form carries it: the tab posts every part
// it draws, and a form that carries fewer, such as one drawn before a
// part existed, leaves the others as they are rather than emptying
// them. The style is returned as typed whether or not it is refused,
// with the first problem found.
func styleFromForm(r *http.Request, current domain.Style) (domain.Style, error) {
	style := current
	if err := r.ParseForm(); err != nil {
		return style, nil
	}
	var problem error
	keep := func(err error) {
		if problem == nil {
			problem = err
		}
	}
	var err error
	if r.PostForm.Has("theme") {
		style, err = style.WithTheme(r.PostFormValue("theme"))
		keep(err)
	}
	if r.PostForm.Has("header_name") {
		style.Header, err = domain.NewStyleHeader(
			r.PostFormValue("header_name"), r.PostFormValue("header_tagline"),
			formLinks(r, "header_link_label", "header_link_url"))
		keep(err)
	}
	if r.PostForm.Has("footer_text") {
		style.Footer, err = domain.NewStyleFooter(
			r.PostFormValue("footer_text"),
			formLinks(r, "footer_link_label", "footer_link_url"))
		keep(err)
	}
	return style, problem
}

// formLinks pairs the label and the address of each link row, in the
// order the rows are on the page.
func formLinks(r *http.Request, labelField, urlField string) []domain.StyleLink {
	labels, urls := r.PostForm[labelField], r.PostForm[urlField]
	links := make([]domain.StyleLink, max(len(labels), len(urls)))
	for i := range links {
		if i < len(labels) {
			links[i].Label = labels[i]
		}
		if i < len(urls) {
			links[i].URL = urls[i]
		}
	}
	return links
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
	// The sheet wears a whole style, so the header and the footer are
	// seen in every theme too.
	style.Header = domain.StyleHeader{
		Name:    "Corner Workshop",
		Tagline: "Evening classes in wood, clay and print.\nTell us how the open day went.",
		Links: []domain.StyleLink{
			{Label: "Our classes", URL: "https://example.com/classes"},
			{Label: "Contact", URL: "https://example.com/contact"},
		},
	}
	style.Footer = domain.StyleFooter{
		Text:  "Corner Workshop Cooperative\n12 Mill Lane, Riverton",
		Links: []domain.StyleLink{{Label: "Privacy notice", URL: "https://example.com/privacy"}},
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
