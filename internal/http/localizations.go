package http

import (
	"net/http"
	"strconv"
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
			Survey:       viewSurvey(survey, s.clock.Now()),
			Languages:    viewLanguages(draft),
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
		s.renderLocalizations(w, r, err.Error(), "")
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
		s.renderLocalizations(w, r, err.Error(), "")
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
	if len(pending) == 0 {
		s.renderLocalizations(w, r, "", "Nothing left to translate in "+domain.LanguageName(lang)+".")
		return
	}

	if err := s.aiMeter.Check(r.Context(), info.WorkspaceID); err != nil {
		s.renderLocalizations(w, r, aiRefusalMessage(err), "")
		return
	}

	translated, chars, err := s.translateQuestions(r, info.WorkspaceID, pending, lang)
	s.recordTranslationUsage(r, info.WorkspaceID, survey.ID, chars)
	if err != nil && len(translated) == 0 {
		s.logger.Error("localization drafting failed", "error", err)
		s.renderLocalizations(w, r, aiRefusalMessage(err), "")
		return
	}

	for identity, text := range translated {
		// Options are left in the source language deliberately: a
		// mistranslated option changes what an answer means, and the
		// creator can edit them here question by question.
		index, _ := draft.IndexOf(identity)
		if err := draft.SetTranslation(lang, identity, text, draft.Questions[index].Options, false); err != nil {
			s.logger.Error("storing a drafted translation failed", "error", err)
		}
	}
	if err := s.surveys.SaveDraft(r.Context(), survey.ID, info.UserID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}
	s.renderLocalizations(w, r, "",
		"Drafted "+plural(len(translated), "translation", "translations")+
			" in "+domain.LanguageName(lang)+". Read each one and save it to mark it reviewed.")
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
			s.renderLocalizations(w, r, err.Error(), "")
			return
		}
		saved++
	}
	if err := s.surveys.SaveDraft(r.Context(), survey.ID, info.UserID, draft, s.clock.Now()); err != nil {
		s.internalError(w, r, "save draft", err)
		return
	}

	notice := "Saved and marked reviewed: " + plural(saved, "translation", "translations") +
		" in " + domain.LanguageName(lang) + "."
	if remaining := len(draft.Pending(lang)); remaining > 0 {
		notice += " " + plural(remaining, "question", "questions") + " still to review."
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
func viewLanguages(draft domain.Draft) []templates.LanguageView {
	out := make([]templates.LanguageView, 0, len(draft.Localizations))
	for _, lang := range draft.Languages() {
		localization := draft.Localizations[lang]
		pending := draft.Pending(lang)
		view := templates.LanguageView{
			Code:         lang,
			Name:         domain.LanguageName(lang),
			Total:        len(draft.Questions),
			Reviewed:     len(draft.Questions) - len(pending),
			PendingCount: len(pending),
			Ready:        len(pending) == 0 && len(draft.Questions) > 0,
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

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
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

	chosen := domain.NormalizeLang(r.URL.Query().Get("lang"))
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
}

// viewLanguageChoices offers the languages this version was published
// with, ordered so the browser's own preference comes first — suggested,
// never chosen for them, and never remembered.
func viewLanguageChoices(version store.ServedVersion, r *http.Request) []templates.LanguageChoice {
	if len(version.Languages) == 0 {
		return nil
	}
	preferred := preferredLanguage(r.Header.Get("Accept-Language"), version.Languages)

	choices := []templates.LanguageChoice{{
		Code:     "",
		Name:     "Original",
		Selected: version.Lang == "",
	}}
	for _, lang := range version.Languages {
		choices = append(choices, templates.LanguageChoice{
			Code:      lang,
			Name:      domain.LanguageName(lang),
			Selected:  version.Lang == lang,
			Suggested: version.Lang == "" && lang == preferred,
		})
	}
	return choices
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
		s.renderResults(w, r, survey, results, aiRefusalMessage(err))
		return
	}

	notice := "Translated " + plural(translated, "answer", "answers") + " into " + domain.LanguageName(lang) + "."
	if err != nil {
		notice += " The rest stopped early: " + aiRefusalMessage(err)
	}
	http.Redirect(w, r, "/surveys/"+survey.ID.String()+"/results?lang="+lang+"&notice=translated",
		http.StatusSeeOther)
	_ = notice
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
	return "an unnamed model"
}
