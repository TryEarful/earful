package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// WorkspaceStyle is the account's style (ADR-0023): the theme, header,
// footer and thanks picture that every survey of the workspace follows
// until it makes a part its own, and the style's words in each language
// they have been translated into. The zero value is no account style:
// the workspace's surveys are drawn as they were before there was one.
type WorkspaceStyle struct {
	Style Style
	// Localizations are the style's words by language, each with the
	// wording it was translated from, as a survey's are.
	Localizations map[string]LocalizedStyle
	// UpdatedAt is when the style or its translations last changed, zero
	// where they never have.
	UpdatedAt time.Time
}

// SetStyle replaces the account's style. A picture of the account's left
// behind by a change of the thanks page's choice is dropped first, as a
// survey's is, so the style refers only to pictures its pages show.
func (w *WorkspaceStyle) SetStyle(s Style) error {
	s.Thanks = s.Thanks.settled()
	if err := s.Validate(); err != nil {
		return err
	}
	w.Style = s
	return nil
}

// HasWords reports whether the account's style carries any words a
// language would word differently, and so anything to translate.
func (w WorkspaceStyle) HasWords() bool { return !w.Style.Words().IsZero() }

// Languages lists the languages the account's style has been translated
// into, in a stable order.
func (w WorkspaceStyle) Languages() []string {
	langs := make([]string, 0, len(w.Localizations))
	for lang := range w.Localizations {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}

// SetTranslation records the account style's words in one language, as
// read by the creator when reviewed is true. As with a survey's, a part
// the account's style does not have is dropped, so a translation never
// shows more than the original.
func (w *WorkspaceStyle) SetTranslation(lang string, words StyleWords, reviewed bool) error {
	return w.SetTranslationOf("", lang, words, reviewed)
}

// ErrAccountWordsChanged refuses a translation of the account style's
// words made against wording that has changed since: reviewed, it would
// be marked as read beside words the creator never saw.
var ErrAccountWordsChanged = errors.New("your account style's words changed while this page was open, so read the new wording and save again")

// Fingerprint names these words as they stand, so a form can say which
// wording it was drawn from and a save can tell whether that wording has
// changed since.
func (w StyleWords) Fingerprint() string {
	encoded, _ := json.Marshal(w) // a struct of strings always encodes
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:16])
}

// SetTranslationOf is SetTranslation for a translation made against the
// wording source names, a Fingerprint of the account style's words. A
// wording changed since is refused with ErrAccountWordsChanged; an empty
// source asks no such question.
func (w *WorkspaceStyle) SetTranslationOf(source, lang string, words StyleWords, reviewed bool) error {
	if source != "" && source != w.Style.Words().Fingerprint() {
		return ErrAccountWordsChanged
	}
	lang = NormalizeLang(lang)
	if !ValidLang(lang) {
		return ErrLanguageInvalid
	}
	t, err := newLocalizedStyle(w.Style.Words(), words, reviewed)
	if err != nil {
		return err
	}
	if w.Localizations == nil {
		w.Localizations = map[string]LocalizedStyle{}
	}
	w.Localizations[lang] = t
	return nil
}

// RemoveLanguage drops the account style's words in one language.
func (w *WorkspaceStyle) RemoveLanguage(lang string) {
	delete(w.Localizations, NormalizeLang(lang))
	if len(w.Localizations) == 0 {
		w.Localizations = nil
	}
}

// Pending reports whether a language still needs the account style's
// words translated or reviewed: never translated, not yet read, missing a
// part the style has, or made from a wording since changed.
func (w WorkspaceStyle) Pending(lang string) bool { return w.PendingFor(lang, AllStyleParts) }

// PendingFor is Pending for the words of some parts only. A survey that
// follows the account's header and footer and has a thanks picture of its
// own waits on the account's header and footer in its languages, and
// not on the account's thanks picture, which it does not show.
func (w WorkspaceStyle) PendingFor(lang string, parts StyleParts) bool {
	source := w.Style.Words().Only(parts)
	if source.IsZero() {
		return false
	}
	t, ok := w.Localizations[NormalizeLang(lang)]
	if !ok {
		return true
	}
	return !t.Reviewed || !t.Source.Only(parts).Equal(source) || !t.StyleWords.Only(parts).covers(source)
}

// Stale reports whether a language's words were translated from a
// wording of the account's style that has since changed.
func (w WorkspaceStyle) Stale(lang string) bool {
	t, ok := w.Localizations[NormalizeLang(lang)]
	if !ok {
		return false
	}
	return !t.IsZero() && !t.Source.Equal(w.Style.Words())
}
