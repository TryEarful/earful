package http

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/templates"
)

// accountStyleImages is where the account's style page fetches its
// pictures from: the account's own, which no address serves to anybody
// else.
const accountStyleImages = "/account/style-image/"

// accountStylePage is the account's style (ADR-0023): the theme, header,
// footer and thanks picture every survey of the workspace follows until
// it makes a part its own. A save redirects here and names its outcome
// in the query, as the Style tab's does.
func (s *server) accountStylePage(w http.ResponseWriter, r *http.Request) {
	notice := ""
	query := r.URL.Query()
	if query.Get("notice") == "saved" {
		notice = say(r, "account.style.notice.saved")
	}
	if n, err := strconv.Atoi(query.Get("applied")); err == nil && n >= 0 {
		notice = sayN(r, "account.style.apply.notice.applied", n)
	}
	s.renderAccountStyle(w, r, nil, nil, notice, false)
}

// renderAccountStyle draws the account's style page, with what was typed
// and why it was refused when a save was, as renderSurveyStyle does.
func (s *server) renderAccountStyle(w http.ResponseWriter, r *http.Request, typed *domain.Style, problem error, notice string, filesLost bool) {
	info, _ := authFrom(r.Context())
	current, err := s.surveys.WorkspaceStyle(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "load workspace style", err)
		return
	}
	data := templates.AccountStyleData{
		StyleForm: templates.StyleForm{Style: current.Style, Account: true},
		Notice:    notice,
	}
	// Surveys open now that show an earlier version of the account's
	// style can be brought up to date from here, whenever the creator
	// chooses; publishing is refused while the workspace is suspended.
	if !info.WorkspaceSuspended && problem == nil {
		stale, err := s.surveys.StaleSurveys(r.Context(), info.WorkspaceID, current, s.clock.Now())
		if err != nil {
			s.internalError(w, r, "list stale surveys", err)
			return
		}
		data.StaleCount = len(stale)
		for _, survey := range stale {
			if !survey.Held() {
				data.StaleReady++
			}
		}
	}
	status := http.StatusOK
	if problem != nil {
		status = http.StatusUnprocessableEntity
		data.StyleForm = refusedStyleForm(r, data.StyleForm, typed, problem, filesLost)
	}
	r = r.WithContext(templates.WithStyleImages(r.Context(), accountStyleImages))
	render(w, r, status, templates.AccountStyle(info.Email, info.WorkspaceName, info.CSRFToken, data))
}

// refusedStyleForm is a style form after a refused save: what was typed,
// with the pictures that are stored rather than any that came with the
// form, since one uploaded with a refused form was not stored and there
// is nothing at its address to show; and the problem, worded for the
// head of the form and for beside its field.
func refusedStyleForm(r *http.Request, form templates.StyleForm, typed *domain.Style, problem error, filesLost bool) templates.StyleForm {
	if typed != nil {
		saved := form.Style
		form.Style = *typed
		form.Style.Header.Banner, form.Style.Header.Logo = saved.Header.Banner, saved.Header.Logo
		form.Style.Thanks.Image = saved.Thanks.Image
	}
	form.FilesLost = filesLost
	form.Error = sayErrorAlone(r, problem)
	var where domain.StyleError
	if errors.As(problem, &where) && where.Part != domain.StyleTheme {
		form.ErrorPart, form.ErrorLink = where.Part, where.Link
		form.Error = say(r, styleErrorPlace(where), uitext.Args{"Position": where.Link, "Problem": sayError(r, where.Err)})
		form.FieldError = sayErrorAlone(r, where.Err)
	}
	return form
}

// accountStyleSave saves the account's style. It changes no published
// survey: each takes the account's style when it is next published
// (ADR-0023). The form is read as the Style tab's is, part by part over
// the style it edits, and its pictures are stored with the style in one
// transaction.
func (s *server) accountStyleSave(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	current, err := s.surveys.WorkspaceStyle(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "load workspace style", err)
		return
	}
	before := current
	style, err := styleFromForm(r, current.Style, domain.StyleParts{})
	style, pictures, pictureErr := s.stylePicturesFromForm(r, style, domain.StyleParts{})
	if err == nil {
		err = pictureErr
	}
	if err == nil {
		err = current.SetStyle(style)
	}
	if err == nil {
		err = s.surveys.SaveWorkspaceStyle(r.Context(), info.WorkspaceID, info.UserID, current.Style, storedPictures(pictures), s.clock.Now())
		err = pictureLimitPart(err, pictures)
		if err != nil && !isUserError(err) {
			s.internalError(w, r, "save workspace style", err)
			return
		}
	}
	if err != nil {
		if !isUserError(err) {
			s.internalError(w, r, "set workspace style", err)
			return
		}
		s.renderAccountStyle(w, r, &style, err, "", styleFilesChosen(r))
		return
	}
	if s.askToApply(w, r, before, "?notice=saved") {
		return
	}
	http.Redirect(w, r, "/account/style?notice=saved", http.StatusSeeOther)
}

// askToApply sends the creator, after a change to the account's style,
// to the question of whether to apply it now to the open surveys it
// reaches, when it reaches any (ADR-0023). query carries the notice that
// names what was saved. It reports whether it answered the request.
func (s *server) askToApply(w http.ResponseWriter, r *http.Request, before domain.WorkspaceStyle, query string) bool {
	info, _ := authFrom(r.Context())
	if info.WorkspaceSuspended {
		return false
	}
	after, err := s.surveys.WorkspaceStyle(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "load workspace style", err)
		return true
	}
	affected, err := s.surveys.ActiveSurveysAffected(r.Context(), info.WorkspaceID, before, after, s.clock.Now())
	if err != nil {
		s.internalError(w, r, "list affected surveys", err)
		return true
	}
	if len(affected) == 0 {
		return false
	}
	http.Redirect(w, r, "/account/style/apply"+query, http.StatusSeeOther)
	return true
}

// accountStyleApplyPage asks whether to apply the account's style now to
// the open surveys that show an earlier version of it. Each would go out
// again from its live version, its draft untouched. A survey whose
// languages the account's style is not read in yet is named, with where
// to read it. With none left to ask about, it goes back to the style.
func (s *server) accountStyleApplyPage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	// Where the creator goes when nothing is left to ask about: back to
	// what they saved, with the same notice.
	notice, back := "", "/account/style"
	switch {
	case query.Get("notice") == "saved":
		notice, back = say(r, "account.style.notice.saved"), "/account/style?notice=saved"
	case query.Get("saved") != "":
		lang := domain.NormalizeLang(query.Get("saved"))
		notice = say(r, "account.style.languages.notice.saved", uitext.Args{"Language": languageName(text(r), lang)})
		back = "/account/style/languages?saved=" + url.QueryEscape(lang) + "#lang-" + url.PathEscape(lang)
	case query.Get("notice") == "changed":
		notice = say(r, "account.style.apply.notice.changed")
	}
	if n, err := strconv.Atoi(query.Get("applied")); err == nil && n >= 0 {
		notice = sayN(r, "account.style.apply.notice.applied", n)
		if query.Get("notice") == "changed" {
			// Some went out before the account's style changed under the
			// request; the rest are asked about again.
			notice = sayN(r, "account.style.apply.notice.changed_after", n)
		}
		back = "/account/style?applied=" + strconv.Itoa(n)
	}
	s.renderAccountStyleApply(w, r, notice, back)
}

func (s *server) renderAccountStyleApply(w http.ResponseWriter, r *http.Request, notice, back string) {
	info, _ := authFrom(r.Context())
	account, err := s.surveys.WorkspaceStyle(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "load workspace style", err)
		return
	}
	var stale []store.StaleSurvey
	if !info.WorkspaceSuspended {
		if stale, err = s.surveys.StaleSurveys(r.Context(), info.WorkspaceID, account, s.clock.Now()); err != nil {
			s.internalError(w, r, "list stale surveys", err)
			return
		}
	}
	if len(stale) == 0 {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	l := text(r)
	data := templates.AccountStyleApplyData{
		Notice:    notice,
		UpdatedAt: strconv.FormatInt(account.UpdatedAt.UnixMicro(), 10),
	}
	for _, survey := range stale {
		view := templates.StaleSurveyView{ID: survey.ID.String(), Title: survey.Title, Hold: string(survey.Hold)}
		switch survey.Hold {
		case "":
			data.Ready++
		case store.HoldLanguage:
			view.BlockedIn = survey.BlockedIn
			view.BlockedName = languageName(l, survey.BlockedIn)
		case store.HoldPictures:
			view.Limit = store.MaxSurveyImages
		}
		data.Surveys = append(data.Surveys, view)
	}
	render(w, r, http.StatusOK, templates.AccountStyleApply(info.Email, info.WorkspaceName, info.CSRFToken, data))
}

// accountStyleApply publishes again, from its live version, each survey
// the creator agreed to update, with the account's style as it stands.
// An account's style changed since the creator was asked is not applied:
// they are asked again about the style as it now is.
func (s *server) accountStyleApply(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	var ids []uuid.UUID
	for _, raw := range r.PostForm["survey"] {
		id, err := uuid.Parse(raw)
		if err != nil {
			s.surveyNotFound(w, r)
			return
		}
		ids = append(ids, id)
	}
	updatedAt, err := strconv.ParseInt(r.PostFormValue("updated_at"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/account/style/apply?notice=changed", http.StatusSeeOther)
		return
	}
	result, err := s.surveys.RefreshFromAccount(r.Context(), info.WorkspaceID, ids, updatedAt, info.UserID, s.clock.Now())
	switch {
	case errors.Is(err, store.ErrAccountStyleChanged):
		// Surveys that went out before the change are named, and the rest
		// asked about again with the style as it now is.
		where := "/account/style/apply?notice=changed"
		if len(result.Updated) > 0 {
			where += "&applied=" + strconv.Itoa(len(result.Updated))
		}
		http.Redirect(w, r, where, http.StatusSeeOther)
		return
	case errors.Is(err, store.ErrNotFound):
		s.surveyNotFound(w, r)
		return
	case err != nil:
		s.internalError(w, r, "apply account style", err)
		return
	}
	// Surveys left behind, held back by a language or by their pictures,
	// are asked about again with what holds them; otherwise the style
	// page says how many went out.
	applied := strconv.Itoa(len(result.Updated))
	if len(result.Skipped) > 0 {
		http.Redirect(w, r, "/account/style/apply?applied="+applied, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/account/style?applied="+applied, http.StatusSeeOther)
}

// storedPictures are the prepared pictures, as the store takes them.
func storedPictures(pictures []newPicture) []store.NewImage {
	stored := make([]store.NewImage, len(pictures))
	for i, p := range pictures {
		stored[i] = p.stored
	}
	return stored
}

// pictureLimitPart names the part of the style whose picture did not fit
// under a limit of stored pictures, so the problem is shown beside its
// field. Any other error is returned as it is.
func pictureLimitPart(err error, pictures []newPicture) error {
	var full store.ImageLimitError
	if !errors.As(err, &full) {
		return err
	}
	for _, p := range pictures {
		if p.stored.SHA256 == full.SHA256 {
			return domain.StyleError{Part: p.part, Err: err}
		}
	}
	return err
}

// --- the account style's words in other languages (ADR-0023) ---------------
//
// The account's words are translated once, here, and every survey that
// follows a part of the account's style uses them. A language is offered
// when one of the workspace's surveys is being translated into it, and
// stays while it holds a translation. Reviewing works as on a survey's
// Languages tab: a drafted translation is stored unreviewed, and saving
// is the act of having read it.

// accountStyleLanguagesPage lists the languages the account's style is
// translated into, or ought to be.
func (s *server) accountStyleLanguagesPage(w http.ResponseWriter, r *http.Request) {
	notice := ""
	if lang := r.URL.Query().Get("saved"); lang != "" {
		notice = say(r, "account.style.languages.notice.saved", uitext.Args{"Language": languageName(text(r), domain.NormalizeLang(lang))})
	}
	s.renderAccountStyleLanguages(w, r, http.StatusOK, "", notice)
}

func (s *server) renderAccountStyleLanguages(w http.ResponseWriter, r *http.Request, status int, errMsg, notice string) {
	info, _ := authFrom(r.Context())
	ws, carried, ok := s.accountStyleLanguages(w, r)
	if !ok {
		return
	}
	l := text(r)
	data := templates.AccountStyleLanguagesData{
		HasWords:     ws.HasWords(),
		CanTranslate: s.canTranslate(),
		Error:        errMsg,
		Notice:       notice,
	}
	source := ws.Style.Words()
	data.Source = source.Fingerprint()
	for _, lang := range offeredStyleLanguages(ws, carried) {
		view := templates.AccountStyleLanguageView{
			Code:    lang,
			Name:    languageName(l, lang),
			Label:   languageLabel(l, lang),
			Carried: slices.Contains(carried, lang),
			Style: templates.LocalizedStyleView{
				Source:   source,
				Reviewed: !ws.Pending(lang),
				Stale:    ws.Stale(lang),
			},
		}
		if translated, ok := ws.Localizations[lang]; ok {
			view.Style.Words = translated.StyleWords
		}
		data.Languages = append(data.Languages, view)
	}
	render(w, r, status, templates.AccountStyleLanguages(info.Email, info.WorkspaceName, info.CSRFToken, data))
}

// accountStyleLanguages loads the account's style and the languages the
// workspace's surveys are being translated into.
func (s *server) accountStyleLanguages(w http.ResponseWriter, r *http.Request) (domain.WorkspaceStyle, []string, bool) {
	info, _ := authFrom(r.Context())
	ws, err := s.surveys.WorkspaceStyle(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "load workspace style", err)
		return ws, nil, false
	}
	carried, err := s.surveys.WorkspaceDraftLanguages(r.Context(), info.WorkspaceID)
	if err != nil {
		s.internalError(w, r, "list workspace languages", err)
		return ws, nil, false
	}
	return ws, carried, true
}

// offeredStyleLanguages are the languages the account's style is offered
// in: every one a survey is being translated into, and every one that
// already holds a translation, each once, in order.
func offeredStyleLanguages(ws domain.WorkspaceStyle, carried []string) []string {
	langs := slices.Clone(carried)
	for _, lang := range ws.Languages() {
		if !slices.Contains(langs, lang) {
			langs = append(langs, lang)
		}
	}
	slices.Sort(langs)
	return langs
}

// accountStyleLanguage is the language a request names, when it is one
// the account's style is offered in. Any other is refused with the page
// and a word on why, so a language cannot be started by address alone.
func (s *server) accountStyleLanguage(w http.ResponseWriter, r *http.Request) (domain.WorkspaceStyle, string, bool, bool) {
	ws, carried, ok := s.accountStyleLanguages(w, r)
	if !ok {
		return ws, "", false, false
	}
	lang := domain.NormalizeLang(r.PathValue("lang"))
	if !slices.Contains(offeredStyleLanguages(ws, carried), lang) {
		s.renderAccountStyleLanguages(w, r, http.StatusNotFound, say(r, "account.style.languages.error.unknown"), "")
		return ws, "", false, false
	}
	return ws, lang, slices.Contains(carried, lang), true
}

// accountStyleTranslationSave records the creator's words in a language
// and marks them reviewed, which is what lets a survey that follows the
// account's style publish in that language.
func (s *server) accountStyleTranslationSave(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	ws, lang, _, ok := s.accountStyleLanguage(w, r)
	if !ok {
		return
	}
	if !ws.HasWords() {
		s.renderAccountStyleLanguages(w, r, http.StatusUnprocessableEntity, say(r, "account.style.languages.error.nothing"), "")
		return
	}
	words := domain.StyleWords{
		Tagline:     r.PostFormValue("style_tagline"),
		LogoAlt:     r.PostFormValue("style_logo_alt"),
		FooterText:  r.PostFormValue("style_footer_text"),
		HeaderLinks: r.PostForm["style_header_link"],
		FooterLinks: r.PostForm["style_footer_link"],
		ThanksAlt:   r.PostFormValue("style_thanks_alt"),
	}
	if blankWords(words) {
		s.renderAccountStyleLanguages(w, r, http.StatusUnprocessableEntity, say(r, "account.style.languages.error.blank", uitext.Args{"Language": languageName(text(r), lang)}), "")
		return
	}
	err := s.surveys.SaveWorkspaceStyleTranslation(r.Context(), info.WorkspaceID, info.UserID, s.clock.Now(), func(current *domain.WorkspaceStyle) error {
		return current.SetTranslationOf(sourceOf(r), lang, words, true)
	})
	if err != nil {
		if errors.Is(err, domain.ErrAccountWordsChanged) {
			s.renderAccountStyleLanguages(w, r, http.StatusConflict, sayErrorAlone(r, domain.ErrAccountWordsChanged), "")
			return
		}
		if isUserError(err) {
			s.renderAccountStyleLanguages(w, r, http.StatusUnprocessableEntity, sayErrorAlone(r, err), "")
			return
		}
		s.internalError(w, r, "save workspace style translation", err)
		return
	}
	// A reviewed translation can bring the account's words in that
	// language to the surveys open now, as a change to the style can.
	if s.askToApply(w, r, ws, "?saved="+lang) {
		return
	}
	http.Redirect(w, r, "/account/style/languages?saved="+lang+"#lang-"+lang, http.StatusSeeOther)
}

// sourceOf is the account style's wording a translation form was drawn
// from, as a Fingerprint. A form without one asks no question of it.
func sourceOf(r *http.Request) string {
	return r.PostFormValue("source")
}

// accountStyleTranslationDraft asks the model for a first pass at the
// account style's words in a language and stores it unreviewed, as a
// survey's are drafted. The usage is the workspace's, for no survey.
func (s *server) accountStyleTranslationDraft(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	ws, lang, _, ok := s.accountStyleLanguage(w, r)
	if !ok {
		return
	}
	name := languageName(text(r), lang)
	if !ws.Pending(lang) {
		s.renderAccountStyleLanguages(w, r, http.StatusOK, "", say(r, "languages.notice.nothing", uitext.Args{"Language": name}))
		return
	}
	// A page drawn from wording that has changed since is shown again,
	// before the model is asked for anything.
	if posted := sourceOf(r); posted != "" && posted != ws.Style.Words().Fingerprint() {
		s.renderAccountStyleLanguages(w, r, http.StatusConflict, sayErrorAlone(r, domain.ErrAccountWordsChanged), "")
		return
	}
	if err := s.aiMeter.Check(r.Context(), info.WorkspaceID); err != nil {
		s.renderAccountStyleLanguages(w, r, http.StatusOK, aiRefusalMessage(text(r), err), "")
		return
	}
	source := ws.Style.Words()
	translated, chars, err := s.translateQuestions(r, info.WorkspaceID, styleToTranslate(source), lang)
	s.recordTranslationUsage(r, info.WorkspaceID, nil, chars)
	words, drafted := draftedStyleWords(source, translated)
	if !drafted {
		if err == nil {
			err = errors.New("no words drafted")
		}
		s.logger.Error("account style drafting failed", "error", err)
		s.renderAccountStyleLanguages(w, r, http.StatusOK, aiRefusalMessage(text(r), err), "")
		return
	}
	// The draft is of the words read above, so it is stored only if the
	// account still says them, whatever the form posted.
	from := source.Fingerprint()
	saveErr := s.surveys.SaveWorkspaceStyleTranslation(r.Context(), info.WorkspaceID, info.UserID, s.clock.Now(), func(current *domain.WorkspaceStyle) error {
		return current.SetTranslationOf(from, lang, words, false)
	})
	if errors.Is(saveErr, domain.ErrAccountWordsChanged) {
		s.renderAccountStyleLanguages(w, r, http.StatusConflict, sayErrorAlone(r, domain.ErrAccountWordsChanged), "")
		return
	}
	if saveErr != nil && !isUserError(saveErr) {
		s.internalError(w, r, "save drafted workspace style translation", saveErr)
		return
	}
	if saveErr != nil {
		s.renderAccountStyleLanguages(w, r, http.StatusUnprocessableEntity, sayErrorAlone(r, saveErr), "")
		return
	}
	s.renderAccountStyleLanguages(w, r, http.StatusOK, "", say(r, "account.style.languages.notice.drafted", uitext.Args{"Language": name}))
}

// accountStyleTranslationRemove drops a language no survey is being
// translated into any longer. One a survey carries stays, since that
// survey would need it to publish.
func (s *server) accountStyleTranslationRemove(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	_, lang, carried, ok := s.accountStyleLanguage(w, r)
	if !ok {
		return
	}
	if carried {
		s.renderAccountStyleLanguages(w, r, http.StatusUnprocessableEntity, say(r, "account.style.languages.error.carried", uitext.Args{"Language": languageName(text(r), lang)}), "")
		return
	}
	err := s.surveys.SaveWorkspaceStyleTranslation(r.Context(), info.WorkspaceID, info.UserID, s.clock.Now(), func(current *domain.WorkspaceStyle) error {
		current.RemoveLanguage(lang)
		return nil
	})
	if err != nil {
		s.internalError(w, r, "remove workspace style language", err)
		return
	}
	http.Redirect(w, r, "/account/style/languages", http.StatusSeeOther)
}

// accountStyleImage serves one of the account's pictures to its own
// creator, for the account's style page. Another workspace's are not
// found, and nobody without a session reaches this.
func (s *server) accountStyleImage(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	img, err := s.surveys.WorkspaceImage(r.Context(), info.WorkspaceID, r.PathValue("sha256"))
	if !s.styleImageFound(w, r, err) {
		return
	}
	if styleImageHeaders(w, r, img.SHA256, draftImageCache) {
		return
	}
	writeStyleImage(w, r, img)
}
