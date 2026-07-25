package domain

import (
	"errors"
	"sort"
	"strings"
)

// Localizations (M11-T1) live in the draft while they are being written
// and are frozen into the published version at publish, exactly like the
// questions they translate.
//
// The rule that shapes this type is story 23's: a machine translation
// must not go out in a creator's name until they have read it. So a
// language carries a Reviewed flag per question, and publishing refuses
// while any of them is false. "Reviewed" here means a person looked;
// nothing pretends to check the translation's quality.

// Localization is one language's version of a draft's questions.
type Localization struct {
	// Lang is a BCP-47-ish subtag: "nl", "pt-BR".
	Lang string `json:"lang"`
	// Questions is keyed by Question Identity, so a rewording in the
	// source language keeps its translation attached (and marks it
	// unreviewed again — see MarkStale).
	Questions map[string]LocalizedQuestion `json:"questions"`
}

// LocalizedQuestion is one question in one language.
type LocalizedQuestion struct {
	Text    string   `json:"text"`
	Options []string `json:"options,omitempty"`
	// Reviewed is set when a creator has read this translation. It is
	// cleared whenever the source question changes, because a
	// translation of an old wording has not been reviewed.
	Reviewed bool `json:"reviewed"`
	// SourceText is the wording this translation was made from, which is
	// how a change in the source is detected.
	SourceText string `json:"source_text"`
}

var (
	ErrUnknownLanguage  = errors.New("that language is not part of this survey")
	ErrLanguageInvalid  = errors.New("use a language code like \"nl\" or \"pt-BR\"")
	ErrLanguageExists   = errors.New("that language is already on this survey")
	ErrUnreviewedTrans  = errors.New("review every translation before publishing, or remove the language")
	ErrTooManyLanguages = errors.New("a survey can carry at most 10 languages")
)

const maxLanguagesPerSurvey = 10

// AddLanguage starts a language with empty, unreviewed translations.
func (d *Draft) AddLanguage(lang string) error {
	lang = NormalizeLang(lang)
	if !ValidLang(lang) {
		return ErrLanguageInvalid
	}
	if d.HasLanguage(lang) {
		return ErrLanguageExists
	}
	if len(d.Localizations) >= maxLanguagesPerSurvey {
		return ErrTooManyLanguages
	}
	if d.Localizations == nil {
		d.Localizations = map[string]Localization{}
	}
	d.Localizations[lang] = Localization{Lang: lang, Questions: map[string]LocalizedQuestion{}}
	return nil
}

// RemoveLanguage drops a language and its translations.
func (d *Draft) RemoveLanguage(lang string) error {
	lang = NormalizeLang(lang)
	if !d.HasLanguage(lang) {
		return ErrUnknownLanguage
	}
	delete(d.Localizations, lang)
	return nil
}

func (d Draft) HasLanguage(lang string) bool {
	_, ok := d.Localizations[NormalizeLang(lang)]
	return ok
}

// Languages lists the draft's languages in a stable order.
func (d Draft) Languages() []string {
	langs := make([]string, 0, len(d.Localizations))
	for lang := range d.Localizations {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}

// SetTranslation records one translated question. reviewed is what the
// creator said: drafting sets it false, saving an edit sets it true.
func (d *Draft) SetTranslation(lang, identityID, text string, options []string, reviewed bool) error {
	lang = NormalizeLang(lang)
	localization, ok := d.Localizations[lang]
	if !ok {
		return ErrUnknownLanguage
	}
	index, found := d.IndexOf(identityID)
	if !found {
		return ErrQuestionUnknown
	}
	if localization.Questions == nil {
		localization.Questions = map[string]LocalizedQuestion{}
	}
	localization.Questions[identityID] = LocalizedQuestion{
		Text:       strings.TrimSpace(text),
		Options:    options,
		Reviewed:   reviewed,
		SourceText: d.Questions[index].Text,
	}
	d.Localizations[lang] = localization
	return nil
}

// Pending lists the questions in a language that still need a
// translation or a review — either never translated, or translated from
// a wording the creator has since changed.
func (d Draft) Pending(lang string) []Question {
	localization, ok := d.Localizations[NormalizeLang(lang)]
	if !ok {
		return nil
	}
	var pending []Question
	for _, question := range d.Questions {
		translated, exists := localization.Questions[question.IdentityID]
		if !exists || !translated.Reviewed || translated.SourceText != question.Text {
			pending = append(pending, question)
		}
	}
	return pending
}

// ReadyToPublish reports whether every language is fully reviewed
// against the current wording. This is the gate story 23 asks for: no
// unreviewed machine translation is ever published.
func (d Draft) ReadyToPublish() error {
	for _, lang := range d.Languages() {
		if len(d.Pending(lang)) > 0 {
			return ErrUnreviewedTrans
		}
	}
	return nil
}

// NormalizeLang lowercases the subtag and title-cases the region, so
// "PT-br" and "pt-BR" are one language.
func NormalizeLang(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return ""
	}
	parts := strings.SplitN(lang, "-", 2)
	out := strings.ToLower(parts[0])
	if len(parts) == 2 && parts[1] != "" {
		out += "-" + strings.ToUpper(parts[1])
	}
	return out
}

// ValidLang accepts the shapes a respondent's browser and a creator's
// keyboard actually produce: "nl", "pt-BR".
func ValidLang(lang string) bool {
	if len(lang) < 2 || len(lang) > 8 {
		return false
	}
	for i, r := range lang {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// LanguageName gives a readable name for the common cases and falls back
// to the code itself. It is a courtesy, not a locale library.
func LanguageName(lang string) string {
	if name, ok := languageNames[NormalizeLang(lang)]; ok {
		return name
	}
	return lang
}

var languageNames = map[string]string{
	"ar": "Arabic", "cs": "Czech", "da": "Danish", "de": "German", "el": "Greek",
	"en": "English", "es": "Spanish", "fi": "Finnish", "fr": "French", "he": "Hebrew",
	"hi": "Hindi", "hu": "Hungarian", "id": "Indonesian", "it": "Italian", "ja": "Japanese",
	"ko": "Korean", "nb": "Norwegian", "nl": "Dutch", "pl": "Polish", "pt": "Portuguese",
	"pt-BR": "Portuguese (Brazil)", "ro": "Romanian", "ru": "Russian", "sv": "Swedish",
	"tr": "Turkish", "uk": "Ukrainian", "vi": "Vietnamese", "zh": "Chinese",
}
