package domain

import (
	"slices"
	"sort"
)

// An account's style reaches a published survey when the survey is next
// published (ADR-0023). A creator who changes it can also apply the
// change to the surveys that are open now, which publishes each of them
// again from its live version: the questions, the thank you page and the
// translations it went out with, its own parts of the style as they
// were, and the parts it takes from the account as the account now has
// them. Nothing of the survey's draft is published.

// PublishedVersion is what a published version froze that publishing it
// again needs.
type PublishedVersion struct {
	Questions []Question
	// QuestionTranslations is each language's wording of the questions,
	// by language and then by Question Identity. Only Text and Options
	// are read.
	QuestionTranslations map[string]map[string]LocalizedQuestion
	Thanks               ThankYou
	ThanksTranslations   map[string]ThankYou
	// Style is the style the version's pages are drawn in, complete, and
	// StyleTranslations its words in each language it went out in.
	Style             Style
	StyleTranslations map[string]StyleWords
	// Follows names the parts of Style the version took from its
	// account's style. A version published before a survey could follow
	// an account names none.
	Follows StyleParts
}

// Languages lists the languages the version went out in, in order.
func (v PublishedVersion) Languages() []string {
	var langs []string
	add := func(lang string) {
		if !slices.Contains(langs, lang) {
			langs = append(langs, lang)
		}
	}
	for lang := range v.QuestionTranslations {
		add(lang)
	}
	for lang := range v.ThanksTranslations {
		add(lang)
	}
	for lang := range v.StyleTranslations {
		add(lang)
	}
	sort.Strings(langs)
	return langs
}

// Draft is the version as a draft to publish again from. Its own parts of
// the style are the version's, and the parts it took from its account are
// left to the account, so publishing it freezes the account's as they
// stand. Every translation counts as read, as it was before the version
// went out; a word the version showed in the original in some language,
// because it had no translation there, is given the original as its
// translation, which draws the same page.
func (v PublishedVersion) Draft() Draft {
	d := Draft{Questions: slices.Clone(v.Questions), Thanks: v.Thanks}
	if !v.Follows.Theme {
		d.Style.Theme, d.StyleChoice.Own.Theme = v.Style.Theme, true
	}
	if !v.Follows.Header {
		// A header the version neither shows nor takes from its account
		// was turned off, or emptied, which draws the same page.
		if v.Style.Header.IsZero() {
			d.StyleChoice.NoHeader = true
		} else {
			d.Style.Header, d.StyleChoice.Own.Header = v.Style.Header, true
		}
	}
	if !v.Follows.Footer {
		if v.Style.Footer.IsZero() {
			d.StyleChoice.NoFooter = true
		} else {
			d.Style.Footer, d.StyleChoice.Own.Footer = v.Style.Footer, true
		}
	}
	if !v.Follows.Thanks {
		d.Style.Thanks, d.StyleChoice.Own.Thanks = v.Style.Thanks, true
	}

	langs := v.Languages()
	if len(langs) == 0 {
		return d
	}
	ownParts, ownWords := d.ownWordParts(), d.OwnStyleWords()
	d.Localizations = make(map[string]Localization, len(langs))
	for _, lang := range langs {
		localization := Localization{Lang: lang, Questions: make(map[string]LocalizedQuestion, len(v.Questions))}
		for _, q := range v.Questions {
			// A question with no wording stored in the language went out
			// in the original there, and does again.
			translated := v.QuestionTranslations[lang][q.IdentityID]
			localization.Questions[q.IdentityID] = LocalizedQuestion{
				Text: translated.Text, Options: translated.Options, Reviewed: true, SourceText: q.Text,
			}
		}
		if d.HasThanksToTranslate() {
			translated, ok := v.ThanksTranslations[lang]
			if !ok {
				translated = v.Thanks
			}
			localization.Thanks = &LocalizedThanks{
				Message: translated.Message, LinkLabel: translated.LinkLabel, Reviewed: true,
				SourceMessage: v.Thanks.Message, SourceLinkLabel: v.Thanks.LinkLabel,
			}
		}
		if !ownWords.IsZero() {
			words, ok := v.StyleTranslations[lang]
			if !ok {
				words = ownWords
			}
			localization.Style = &LocalizedStyle{StyleWords: words.Only(ownParts), Reviewed: true, Source: ownWords}
		}
		d.Localizations[lang] = localization
	}
	return d
}

// AccountRefresh is what publishing a version again would do, with its
// account's style as it stands.
type AccountRefresh struct {
	// Changes reports whether the version's pages would look different:
	// a part it follows has changed on the account, in the original or
	// in a language it went out in.
	Changes bool
	// BlockedIn names a language the account's style has to be reviewed
	// in before the version can go out again, and is empty when none.
	BlockedIn string
}

// RefreshAgainst says what publishing the version again would do with
// account as it stands. A version that follows nothing of its account's
// would change nothing.
func (v PublishedVersion) RefreshAgainst(account WorkspaceStyle) AccountRefresh {
	if v.Follows.IsZero() {
		return AccountRefresh{}
	}
	d := v.Draft()
	r := AccountRefresh{Changes: !v.drawnAs(d, account)}
	if lang, ok := IsAccountStyleTranslationError(d.ReadyToPublish(account)); ok {
		r.BlockedIn = lang
	}
	return r
}

// ChangedBetween reports whether the version, published again, would be
// drawn differently against after than against before: whether a change
// to the account's style from before to after reaches a part the version
// follows, in the original or in a language it went out in.
func (v PublishedVersion) ChangedBetween(before, after WorkspaceStyle) bool {
	if v.Follows.IsZero() {
		return false
	}
	d := v.Draft()
	if !StyleDiffers(d.ResolvedStyle(before.Style), d.ResolvedStyle(after.Style)).IsZero() {
		return true
	}
	for _, lang := range v.Languages() {
		wordsBefore, okBefore := frozenWords(d, before, lang)
		wordsAfter, okAfter := frozenWords(d, after, lang)
		if okBefore != okAfter || !wordsBefore.Equal(wordsAfter) {
			return true
		}
	}
	return false
}

// drawnAs reports whether d, resolved against account, would be drawn as
// the version is: the same style, and the same words in every language.
func (v PublishedVersion) drawnAs(d Draft, account WorkspaceStyle) bool {
	if !StyleDiffers(v.Style, d.ResolvedStyle(account.Style)).IsZero() {
		return false
	}
	for _, lang := range v.Languages() {
		words, ok := frozenWords(d, account, lang)
		frozen, had := v.StyleTranslations[lang]
		if ok != had || (ok && !words.Equal(frozen)) {
			return false
		}
	}
	return true
}

// frozenWords are the words publishing d against account would freeze in
// lang, and whether it would freeze any: words that are all empty are not
// frozen, as styleColumn leaves them out.
func frozenWords(d Draft, account WorkspaceStyle, lang string) (StyleWords, bool) {
	words, ok := d.ResolvedStyleWords(account, lang)
	if !ok || words.IsZero() {
		return StyleWords{}, false
	}
	return words, true
}
