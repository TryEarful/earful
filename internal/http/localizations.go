package http

import (
	"github.com/TryEarful/earful/internal/uitext"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/web/templates"
)

// Question localization (M11-T1).
//
// Translations are drafted by a model, reviewed by a person, and frozen
// into the published version. The review is not a formality: a
// translation nobody has read goes out in the creator's name, in a
// language they may not speak, to respondents who cannot tell a good
// translation from a confident one. Publishing refuses until every
// selected language is reviewed against the current wording.
//
// Everything lives in the draft until publish, so drafting and editing
// are ordinary draft edits: revisions, audit log, undo, all for free.

func (s *server) localizationsPage(w http.ResponseWriter, r *http.Request) {
	s.renderLocalizations(w, r, "", "")
}

func (s *server) renderLocalizations(w http.ResponseWriter, r *http.Request, errMsg, notice string) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	render(w, r, http.StatusOK, templates.Localizations(info.Email, info.WorkspaceName, info.CSRFToken,
		templates.LocalizationsData{
			Survey:       viewSurvey(text(r), survey, s.clock.Now()),
			Languages:    viewLanguages(text(r), draft),
			Questions:    draft.Questions,
			CanTranslate: s.canTranslate(),
			Error:        errMsg,
			Notice:       notice,
		}))
}

// localizationAdd starts a language.
func (s *server) localizationAdd(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	lang := r.PostFormValue("lang")
	if err := draft.AddLanguage(lang); err != nil {
		s.renderLocalizations(w, r, sayErrorAlone(r, err), "")
		return
	}
	if err := s.surveys.SaveDraft(r.Context(), survey.ID, info.UserID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}
	http.Redirect(w, r, localizationsPath(survey.ID, domain.NormalizeLang(lang)), http.StatusSeeOther)
}

func (s *server) localizationRemove(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	if err := draft.RemoveLanguage(r.PathValue("lang")); err != nil {
		s.renderLocalizations(w, r, sayErrorAlone(r, err), "")
		return
	}
	if err := s.surveys.SaveDraft(r.Context(), survey.ID, info.UserID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}
	http.Redirect(w, r, localizationsPath(survey.ID, ""), http.StatusSeeOther)
}

// localizationDraft asks the model for a first pass at every question
// that needs one, and stores it **unreviewed**. Nothing here can publish
// on its own.
func (s *server) localizationDraft(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	lang := domain.NormalizeLang(r.PathValue("lang"))
	pending := draft.Pending(lang)
	if draft.ThanksPending(lang) {
		pending = append(pending, thanksToTranslate(draft)...)
	}
	if len(pending) == 0 {
		s.renderLocalizations(w, r, "", say(r, "languages.notice.nothing", uitext.Args{"Language": languageName(text(r), lang)}))
		return
	}

	if err := s.aiMeter.Check(r.Context(), info.WorkspaceID); err != nil {
		s.renderLocalizations(w, r, aiRefusalMessage(text(r), err), "")
		return
	}

	translated, chars, err := s.translateQuestions(r, info.WorkspaceID, pending, lang)
	s.recordTranslationUsage(r, info.WorkspaceID, survey.ID, chars)
	if err != nil && len(translated) == 0 {
		s.logger.Error("localization drafting failed", "error", err)
		s.renderLocalizations(w, r, aiRefusalMessage(text(r), err), "")
		return
	}

	var thanksMessage, thanksLabel string
	draftedThanks := false
	for identity, text := range translated {
		switch identity {
		case thanksMessageKey:
			thanksMessage, draftedThanks = text, true
			continue
		case thanksLabelKey:
			thanksLabel, draftedThanks = text, true
			continue
		}
		// Options are left in the source language deliberately: a
		// mistranslated option changes what an answer means, and the
		// creator can edit them here question by question.
		index, _ := draft.IndexOf(identity)
		if err := draft.SetTranslation(lang, identity, text, draft.Questions[index].Options, false); err != nil {
			s.logger.Error("storing a drafted translation failed", "error", err)
		}
	}
	if draftedThanks {
		if err := draft.SetThanksTranslation(lang, thanksMessage, thanksLabel, false); err != nil {
			s.logger.Error("storing a drafted thank you page failed", "error", err)
		}
	}
	if err := s.surveys.SaveDraft(r.Context(), survey.ID, info.UserID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}
	s.renderLocalizations(w, r, "",
		sayN(r, "languages.notice.drafted", len(translated), uitext.Args{"Language": languageName(text(r), lang)}))
}

// The thank you page's message and link label are drafted alongside the
// questions, under keys no Question Identity can take, since identities
// are UUIDs.
const (
	thanksMessageKey = "thanks:message"
	thanksLabelKey   = "thanks:link_label"
)

// thanksToTranslate is the thank you page's wording, shaped as the
// questions translateQuestions takes.
func thanksToTranslate(draft domain.Draft) []domain.Question {
	var out []domain.Question
	if draft.Thanks.Message != "" {
		out = append(out, domain.Question{IdentityID: thanksMessageKey, Text: draft.Thanks.Message})
	}
	if draft.Thanks.LinkLabel != "" {
		out = append(out, domain.Question{IdentityID: thanksLabelKey, Text: draft.Thanks.LinkLabel})
	}
	return out
}

// translateQuestions runs one model call per question, re-checking the
// meter before each. Checking once for the batch would let a survey with
// twenty questions spend twenty times its remaining allowance; this way
// a quota that runs out mid-batch keeps whatever was already translated
// and stops there.
func (s *server) translateQuestions(r *http.Request, workspaceID uuid.UUID,
	questions []domain.Question, lang string) (map[string]string, int, error) {
	out := make(map[string]string, len(questions))
	chars := 0
	for _, question := range questions {
		if err := s.aiMeter.Check(r.Context(), workspaceID); err != nil {
			return out, chars, err
		}
		stream, err := s.ai.Translate(r.Context(), ai.TranslateRequest{
			Text:       question.Text,
			TargetLang: domain.LanguageName(lang),
		})
		if err != nil {
			return out, chars, err
		}
		counted := ai.Counted(stream)
		text, err := ai.Collect(counted)
		chars += counted.Chars() + len(question.Text)
		if err != nil && text == "" {
			return out, chars, err
		}
		out[question.IdentityID] = strings.TrimSpace(text)
	}
	return out, chars, nil
}

// localizationSave records the creator's edits and marks them reviewed —
// the act that makes them publishable (story 23).
func (s *server) localizationSave(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, draft, ok := s.loadSurveyAndDraft(w, r)
	if !ok {
		return
	}
	lang := domain.NormalizeLang(r.PathValue("lang"))
	if !draft.HasLanguage(lang) {
		s.surveyNotFound(w, r)
		return
	}

	saved := 0
	for _, question := range draft.Questions {
		text := strings.TrimSpace(r.PostFormValue("t_" + question.IdentityID))
		if text == "" {
			continue
		}
		options := splitLines(r.PostFormValue("o_" + question.IdentityID))
		if len(options) == 0 {
			options = question.Options
		}
		if err := draft.SetTranslation(lang, question.IdentityID, text, options, true); err != nil {
			s.renderLocalizations(w, r, sayErrorAlone(r, err), "")
			return
		}
		saved++
	}
	if draft.HasThanksToTranslate() {
		message := r.PostFormValue("thanks_message")
		label := r.PostFormValue("thanks_link_label")
		if strings.TrimSpace(message) != "" || strings.TrimSpace(label) != "" {
			if err := draft.SetThanksTranslation(lang, message, label, true); err != nil {
				s.renderLocalizations(w, r, sayErrorAlone(r, err), "")
				return
			}
			saved++
		}
	}
	if err := s.surveys.SaveDraft(r.Context(), survey.ID, info.UserID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}

	notice := sayN(r, "languages.notice.saved", saved, uitext.Args{"Language": languageName(text(r), lang)})
	if remaining := len(draft.Pending(lang)); remaining > 0 {
		notice += " " + sayN(r, "languages.notice.remaining", remaining)
	}
	s.renderLocalizations(w, r, "", notice)
}

func (s *server) recordTranslationUsage(r *http.Request, workspaceID, surveyID uuid.UUID, chars int) {
	id := surveyID
	if err := s.aiMeter.Record(r.Context(), workspaceID, &id, string(ai.OpTranslate), chars); err != nil {
		s.logger.Error("recording translation usage failed", "error", err)
	}
}

func (s *server) canTranslate() bool { return ai.Supports(s.ai, ai.OpTranslate) }

func localizationsPath(surveyID uuid.UUID, lang string) string {
	path := "/surveys/" + surveyID.String() + "/localizations"
	if lang != "" {
		path += "#lang-" + lang
	}
	return path
}

// viewLanguages describes each language's state: how much is translated,
// how much is still waiting for a person to read it.
func viewLanguages(l uitext.Localizer, draft domain.Draft) []templates.LanguageView {
	out := make([]templates.LanguageView, 0, len(draft.Localizations))
	for _, lang := range draft.Languages() {
		localization := draft.Localizations[lang]
		pending := draft.Pending(lang)
		thanksPending := draft.ThanksPending(lang)
		view := templates.LanguageView{
			Code:         lang,
			Name:         languageName(l, lang),
			Label:        languageLabel(l, lang),
			Total:        len(draft.Questions),
			Reviewed:     len(draft.Questions) - len(pending),
			PendingCount: len(pending),
			Ready:        len(pending) == 0 && !thanksPending && len(draft.Questions) > 0,
		}
		if thanksPending {
			view.PendingCount++
		}
		if draft.HasThanksToTranslate() {
			// The thank you page is counted with the questions, so the
			// progress line agrees with the number left to review.
			view.Total++
			if !thanksPending {
				view.Reviewed++
			}
			thanks := templates.LocalizedThanksView{
				SourceMessage:   draft.Thanks.Message,
				SourceLinkLabel: draft.Thanks.LinkLabel,
				Reviewed:        !thanksPending,
				Stale:           draft.ThanksStale(lang),
			}
			if translated := localization.Thanks; translated != nil {
				thanks.Message, thanks.LinkLabel = translated.Message, translated.LinkLabel
			}
			view.Thanks = &thanks
		}
		for _, question := range draft.Questions {
			translated := localization.Questions[question.IdentityID]
			view.Questions = append(view.Questions, templates.LocalizedQuestionView{
				IdentityID:    question.IdentityID,
				SourceText:    question.Text,
				Text:          translated.Text,
				Options:       strings.Join(translated.Options, "\n"),
				SourceOptions: strings.Join(question.Options, "\n"),
				NeedsOptions:  question.Type.NeedsOptions(),
				Reviewed:      translated.Reviewed && translated.SourceText == question.Text,
				Stale:         translated.Text != "" && translated.SourceText != question.Text,
			})
		}
		out = append(out, view)
	}
	return out
}

func splitLines(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// --- the respondent's side (M11-T1, story 25) ----------------------------

// applyLanguage serves the version in the respondent's language when the
// version has one. The choice travels in the URL and is stored nowhere:
// no cookie, no column, nothing that could later say "this person reads
// Dutch" (story 25).
func (s *server) applyLanguage(r *http.Request, version *store.ServedVersion) {
	langs, err := s.surveys.VersionLanguages(r.Context(), version.ID)
	if err != nil {
		s.logger.Error("reading version languages failed", "error", err)
		return
	}
	if len(langs) == 0 {
		return
	}
	version.Languages = langs

	chosen := addressLanguage(r)
	if chosen == "" || !contains(langs, chosen) {
		return
	}
	questions, err := s.surveys.LocalizedQuestions(r.Context(), version.ID, chosen)
	if err != nil {
		s.logger.Error("reading localized questions failed", "error", err)
		return
	}
	version.Questions = questions
	version.Lang = chosen
	// A version whose thank you page was not translated shows it as
	// written, as it does its questions when a language is missing.
	if thanks, ok := version.LocalizedThanks[chosen]; ok {
		version.Thanks = thanks
	}
}

// shownVersion is the version as the respondent saw it: in the language
// their address names, or as written when it names none. The version
// passed in is left as the creator wrote it, because that is what an
// answer is validated against and stored as.
func (s *server) shownVersion(r *http.Request, version store.ServedVersion) store.ServedVersion {
	shown := version
	s.applyLanguage(r, &shown)
	return shown
}

// canonicalAnswers turns choices made on a localized form back into the
// options as the creator wrote them. A form posts the option text it
// displayed, so without this a Dutch answer would fail validation against
// English options, or be counted as a different answer from its English
// twin. Position identifies the option: a localized option set is only
// served when it is complete and in the creator's order. A value that
// matches nothing shown is passed through for validation to refuse.
// Other is no option and is passed through as it is, before any option
// is compared with it, and so is what was written beside it: that is
// the respondent's own text in whatever language they wrote it.
func canonicalAnswers(submitted domain.Submission, shown, original []domain.Question) domain.Submission {
	written := make(map[string][]string, len(original))
	for _, q := range original {
		written[q.IdentityID] = q.Options
	}
	canonical := func(q domain.Question, choice string) string {
		if q.AllowOther && choice == domain.OtherChoice {
			return choice
		}
		options := written[q.IdentityID]
		if len(options) != len(q.Options) {
			return choice
		}
		for i, opt := range q.Options {
			if opt == choice {
				return options[i]
			}
		}
		return choice
	}

	answers := make(map[string]domain.AnswerValue, len(submitted.Answers))
	for identity, value := range submitted.Answers {
		answers[identity] = value
	}
	for _, q := range shown {
		value, ok := answers[q.IdentityID]
		if !ok {
			continue
		}
		if value.Choice != "" {
			value.Choice = canonical(q, value.Choice)
		}
		if len(value.Choices) > 0 {
			choices := make([]string, len(value.Choices))
			for i, choice := range value.Choices {
				choices[i] = canonical(q, choice)
			}
			value.Choices = choices
		}
		answers[q.IdentityID] = value
	}
	return domain.Submission{Answers: answers}
}

// viewLanguageChoices offers every language the interface is written in
// and every language this version was published with, each once, with
// the browser's own preference among the translations suggested: never
// chosen for them, and never remembered. A language the interface is written in is offered by
// the name it gives itself, since whoever looks for it may not read the
// language the page is in. With fewer than two languages there is
// nothing to choose between, and no picker.
func viewLanguageChoices(version store.ServedVersion, r *http.Request) []templates.LanguageChoice {
	l := text(r)
	offered := append([]string(nil), l.Languages()...)
	for _, lang := range version.Languages {
		if !contains(offered, lang) {
			offered = append(offered, lang)
		}
	}
	if len(offered) < 2 {
		return nil
	}
	chosen := chosenLanguage(r, version)
	// Only a translation is suggested: the page is already worded in the
	// browser's language where the interface is written in it, and the
	// questions are what a suggestion would change.
	preferred := preferredLanguage(r.Header.Get("Accept-Language"), version.Languages)

	choices := []templates.LanguageChoice{{
		Code:     "",
		Name:     say(r, "respond.language.original"),
		Selected: chosen == "",
	}}
	for _, lang := range offered {
		choice := templates.LanguageChoice{
			Code:      lang,
			Name:      languageLabel(l, lang),
			Selected:  chosen == lang,
			Suggested: chosen == "" && lang == preferred,
		}
		if id := templates.OwnName(lang); id != "" {
			choice.Name = l.T(id)
			choice.Lang = lang
		}
		choices = append(choices, choice)
	}
	return choices
}

// addressLanguage is the language a respondent's address names: one the
// survey was translated into, one the interface is written in, both, or
// neither. It is the only place the choice is kept (story 25).
func addressLanguage(r *http.Request) string {
	return domain.NormalizeLang(r.URL.Query().Get("lang"))
}

// chosenLanguage is the language the address names when it is one the
// picker offers, and "" when it names none or one nothing is written in.
// It is what the page's own addresses carry on, so that answering, being
// corrected and being thanked all happen in the language chosen.
func chosenLanguage(r *http.Request, version store.ServedVersion) string {
	chosen := addressLanguage(r)
	if contains(version.Languages, chosen) || contains(text(r).Languages(), chosen) {
		return chosen
	}
	return ""
}

// untranslated reports whether the respondent chose a language the
// interface is written in and the survey was not translated into, so the
// page around the questions is in that language and the questions are as
// the creator wrote them. The notice that says so is worded in the
// chosen language, and is given only where the page is in it.
func untranslated(r *http.Request, version store.ServedVersion) bool {
	chosen := chosenLanguage(r, version)
	return chosen != "" && version.Lang == "" && interfaceLanguage(r) == chosen
}

// preferredLanguage reads Accept-Language and returns the best available
// match, or "" — a suggestion only.
func preferredLanguage(header string, available []string) string {
	for _, part := range strings.Split(header, ",") {
		tag := domain.NormalizeLang(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if tag == "" {
			continue
		}
		if contains(available, tag) {
			return tag
		}
		// "nl-BE" should suggest "nl" when only the base language exists.
		if base := strings.SplitN(tag, "-", 2)[0]; contains(available, base) {
			return base
		}
	}
	return ""
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// --- answer translation (M11-T2) -----------------------------------------

// answersTranslate translates a survey's text answers into the language
// the creator asked for, caching each one. The original is never
// touched: a translation is a separate row, shown beside what the
// respondent actually said and marked as machine-made (stories 26, 27).
func (s *server) answersTranslate(w http.ResponseWriter, r *http.Request) {
	info, _ := authFrom(r.Context())
	survey, ok := s.loadSurvey(w, r)
	if !ok {
		return
	}
	lang := domain.NormalizeLang(r.PostFormValue("lang"))
	if !domain.ValidLang(lang) {
		http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/results", http.StatusSeeOther)
		return
	}

	results, err := s.surveys.SurveyResults(r.Context(), survey.ID)
	if err != nil {
		s.internalError(w, r, "load results", err)
		return
	}
	existing, err := s.surveys.AnswerTranslations(r.Context(), survey.ID, lang)
	if err != nil {
		s.internalError(w, r, "load translations", err)
		return
	}

	translated, chars, err := s.translateAnswers(r, info.WorkspaceID, survey.ID, results, existing, lang)
	s.recordTranslationUsage(r, info.WorkspaceID, survey.ID, chars)
	if err != nil && translated == 0 {
		s.renderResults(w, r, survey, results, aiRefusalMessage(text(r), err))
		return
	}

	http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/results?lang="+lang+"&notice=translated",
		http.StatusSeeOther)
}

// translateAnswers walks every text answer that has no cached
// translation in this language. Like question drafting, it re-checks the
// meter per call so a long survey cannot overshoot its allowance.
func (s *server) translateAnswers(r *http.Request, workspaceID, surveyID uuid.UUID,
	results store.Results, existing map[uuid.UUID]store.AnswerTranslation, lang string) (int, int, error) {
	count, chars := 0, 0
	for _, question := range results.Questions {
		if !question.Type.AcceptsVoice() { // long_text and short_text
			continue
		}
		for _, answer := range question.Answers {
			text := strings.TrimSpace(answer.Value.Text)
			if text == "" {
				continue
			}
			if _, cached := existing[answer.ID]; cached {
				continue // already translated: never pay twice
			}
			if err := s.aiMeter.Check(r.Context(), workspaceID); err != nil {
				return count, chars, err
			}
			stream, err := s.ai.Translate(r.Context(), ai.TranslateRequest{
				Text: text, TargetLang: domain.LanguageName(lang),
			})
			if err != nil {
				return count, chars, err
			}
			counted := ai.Counted(stream)
			out, err := ai.Collect(counted)
			chars += counted.Chars() + len(text)
			if err != nil && out == "" {
				return count, chars, err
			}
			if saveErr := s.surveys.SaveAnswerTranslation(r.Context(), store.AnswerTranslation{
				AnswerID: answer.ID, Lang: lang, Text: strings.TrimSpace(out),
				Model: s.translateModelName(),
			}, s.clock.Now()); saveErr != nil {
				return count, chars, saveErr
			}
			count++
		}
	}
	return count, chars, nil
}

func (s *server) translateModelName() string {
	for _, candidate := range []string{s.cfg.AIModelTranslate, s.cfg.AIModel, s.cfg.AIProvider} {
		if candidate != "" && candidate != "none" {
			return candidate
		}
	}
	return unnamedModel
}
