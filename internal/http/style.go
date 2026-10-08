package http

import (
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
	switch r.URL.Query().Get("notice") {
	case "saved":
		notice = say(r, "style.notice.saved")
	case "reset":
		notice = say(r, "style.notice.reset")
	}
	s.renderSurveyStyle(w, r, nil, nil, notice, false)
}

// renderSurveyStyle draws the Style tab. Its fields hold the style the
// survey's pages would be drawn in, its own parts laid over its
// account's (ADR-0023), so a creator changes what they see. typed is
// what the creator entered when the save was refused, shown again in
// place of the draft's style so that nothing has to be typed twice, and
// problem is why. A file is the one thing a refused form cannot hand
// back: filesLost says one was chosen, so the page can say to choose it
// again.
func (s *server) renderSurveyStyle(w http.ResponseWriter, r *http.Request, typed *domain.Style, problem error, notice string, filesLost bool) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	account, err := s.surveys.WorkspaceStyle(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "load workspace style", err)
		return
	}
	data := templates.StyleData{
		Survey: viewSurvey(text(r), survey, s.clock.Now()),
		StyleForm: templates.StyleForm{
			Style: draft.FormStyle(account.Style),
			Choice: &templates.StyleChoiceView{
				Own:        draft.OwnParts(),
				NoHeader:   draft.StyleChoice.NoHeader,
				NoFooter:   draft.StyleChoice.NoFooter,
				HasAccount: !account.Style.IsZero(),
			},
		},
		Notice: notice,
	}
	status := http.StatusOK
	if problem != nil {
		status = http.StatusUnprocessableEntity
		data.StyleForm = refusedStyleForm(r, data.StyleForm, typed, problem, filesLost)
	}
	// The tab shows the draft's pictures, which no version may show yet,
	// and its account's where it follows them.
	r = r.WithContext(templates.WithStyleImages(r.Context(), draftStyleImages(survey.ID)))
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
	case domain.StyleBanner:
		return "style.error.place.banner"
	case domain.StyleLogo:
		return "style.error.place.logo"
	case domain.StyleLogoAlt:
		return "style.error.place.logo_alt"
	case domain.StyleFooterText:
		return "style.error.place.footer_text"
	case domain.StyleFooterLinks:
		if e.Link > 0 {
			return "style.error.place.footer_link"
		}
	case domain.StyleThanksPicture, domain.StyleThanksImage:
		return "style.error.place.thanks_image"
	case domain.StyleThanksAlt:
		return "style.error.place.thanks_alt"
	}
	return "style.error.place.footer_links"
}

// surveyStyleSave saves the style. It is a draft change like a question
// edit: it appends a revision, and respondents see it only once the next
// version is published (ADR-0001), so the page never changes its look
// under somebody who is answering it. A refused style leaves the draft
// as it was.
//
// The form's fields are laid over the style it showed, part by part, so
// that a part of the style this form does not carry is kept. Each part
// it carries is then the survey's own where it differs from the
// account's, and the account's to follow where it does not (ADR-0023).
// "Reset to account style" is the form's second button: it reads no
// field, so a form with a problem in it can still be reset.
func (s *server) surveyStyleSave(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	account, err := s.surveys.WorkspaceStyle(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "load workspace style", err)
		return
	}
	_ = r.ParseForm()
	if r.PostFormValue("style_action") == "reset" {
		draft.ResetStyle()
		if err := s.surveys.SaveStyle(r.Context(), survey.ID, info.UserID, draft, nil, s.clock.Now()); err != nil {
			s.internalError(w, r, "reset style", err)
			return
		}
		http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/style?notice=reset", http.StatusSeeOther)
		return
	}

	header, footer := shownPart(r, "header"), shownPart(r, "footer")
	// A header or a footer turned off is not read: what it held stays as
	// it was, for when it is turned on again, and a problem in it cannot
	// refuse the save.
	skip := domain.StyleParts{Header: header != nil && !*header, Footer: footer != nil && !*footer}
	style, err := styleFromForm(r, draft.FormStyle(account.Style), skip)
	style, pictures, pictureErr := s.stylePicturesFromForm(r, style, skip)
	if err == nil {
		err = pictureErr
	}
	if err == nil {
		err = draft.SetStyleFromForm(account, style, styleCarried(r, skip), header, footer)
	}
	// The pictures are stored only once the whole style is accepted, and
	// with the draft that refers to them, in one transaction.
	if err == nil {
		err = s.surveys.SaveStyle(r.Context(), survey.ID, info.UserID, draft, storedPictures(pictures), s.clock.Now())
		err = pictureLimitPart(err, pictures)
		if err != nil && !isUserError(err) {
			s.internalError(w, r, "save style", err)
			return
		}
	}
	if err != nil {
		if !isUserError(err) {
			s.internalError(w, r, "set style", err)
			return
		}
		s.renderSurveyStyle(w, r, &style, err, "", styleFilesChosen(r))
		return
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/style?notice=saved", http.StatusSeeOther)
}

// shownPart reads the Style tab's "Custom header" or "Custom footer" box.
// An unticked box posts nothing, so each section posts a marker beside
// it: where the marker is there and the box is not, the box was
// unticked. A form without the marker, such as one drawn before the box
// existed, says nothing, and nil leaves the survey as it was.
func shownPart(r *http.Request, part string) *bool {
	if !r.PostForm.Has(part + "_offered") {
		return nil
	}
	shown := r.PostForm.Has("custom_" + part)
	return &shown
}

// styleCarried names the parts of the style the form carried, as
// styleFromForm and stylePicturesFromForm read them, less the parts not
// read.
func styleCarried(r *http.Request, skip domain.StyleParts) domain.StyleParts {
	return domain.StyleParts{
		Theme:  r.PostForm.Has("theme"),
		Header: !skip.Header && (r.PostForm.Has("header_name") || r.PostForm.Has(logoAltField)),
		Footer: !skip.Footer && r.PostForm.Has("footer_text"),
		Thanks: r.PostForm.Has(thanksPictureField) || r.PostForm.Has(thanksAltField),
	}
}

// styleFromForm lays the Style tab's form over the style it edits. A
// part is read only where the form carries it, and not where skip names
// it: the tab posts every part it draws, and a form that carries fewer,
// such as one drawn before a part existed, leaves the others as they are
// rather than emptying them. The style is returned as typed whether or
// not it is refused, with the first problem found.
func styleFromForm(r *http.Request, current domain.Style, skip domain.StyleParts) (domain.Style, error) {
	style := current
	// A form with files is multipart and was parsed on the way in
	// (readUploads); a form without is parsed here.
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
	if r.PostForm.Has("header_name") && !skip.Header {
		var words domain.StyleHeader
		words, err = domain.NewStyleHeader(
			r.PostFormValue("header_name"), r.PostFormValue("header_tagline"),
			formLinks(r, "header_link_label", "header_link_url"))
		// The header's pictures are other fields of the form, and are
		// kept whatever the words become.
		style.Header = style.Header.WithWords(words)
		keep(err)
	}
	if r.PostForm.Has("footer_text") && !skip.Footer {
		style.Footer, err = domain.NewStyleFooter(
			r.PostFormValue("footer_text"),
			formLinks(r, "footer_link_label", "footer_link_url"))
		keep(err)
	}
	if r.PostForm.Has(thanksPictureField) {
		style.Thanks, err = style.Thanks.WithPicture(r.PostFormValue(thanksPictureField))
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
	r, style = withSheetStyleImages(r, style)
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
	// The sheet's footer is a survey's, so it carries the report link
	// too, in every theme. It has no survey, so the link names the sheet.
	if report := templates.ReportFrom(r.Context()); report != nil {
		report.Href = reportMailto(text(r), operatorContact(s.cfg), s.cfg.BaseURL+"/dev/theme-sheet")
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
