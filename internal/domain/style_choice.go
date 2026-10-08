package domain

import (
	"errors"
)

// A survey follows its account's style (ADR-0023) until it makes a part
// its own. The draft keeps the parts that are its own in Style and says
// which they are in StyleChoice; a part it follows holds nothing in
// Style, so that a change to the account's part reaches it. Publishing
// resolves the two into one complete style and freezes that (ADR-0018),
// so a published version never changes with the account.

// StyleChoice is how a survey's style relates to its account's: which
// parts are the survey's own, and whether it shows a header and a footer
// at all.
type StyleChoice struct {
	// Own names the parts of the style that are the survey's own. A part
	// with anything in Style is the survey's own whether or not it is
	// named here, which is how a draft saved before an account could have
	// a style keeps the look it had; the flag is what says so for an own
	// part that is empty, such as Earful's theme chosen under an account
	// drawn in another.
	Own StyleParts `json:"own,omitzero"`
	// NoHeader and NoFooter turn the header or the footer off for this
	// survey, whichever style it would otherwise take them from.
	NoHeader bool `json:"no_header,omitempty"`
	NoFooter bool `json:"no_footer,omitempty"`
}

// IsZero reports whether the survey follows every part of its account's
// style and shows both its header and its footer.
func (c StyleChoice) IsZero() bool { return c == StyleChoice{} }

// OwnParts names the parts of the draft's style that are its own: those
// named as such, and any part with something in it.
func (d Draft) OwnParts() StyleParts {
	own := d.StyleChoice.Own
	return StyleParts{
		Theme:  own.Theme || d.Style.Theme != "",
		Header: own.Header || !d.Style.Header.IsZero(),
		Footer: own.Footer || !d.Style.Footer.IsZero(),
		Thanks: own.Thanks || !d.Style.Thanks.IsZero(),
	}
}

// FollowedParts names the parts a published version of the draft takes
// from the account's style: every part that is not the survey's own,
// except a header or a footer the survey has turned off.
func (d Draft) FollowedParts() StyleParts {
	own := d.OwnParts()
	return StyleParts{
		Theme:  !own.Theme,
		Header: !own.Header && !d.StyleChoice.NoHeader,
		Footer: !own.Footer && !d.StyleChoice.NoFooter,
		Thanks: !own.Thanks,
	}
}

// FormStyle is the style the Style tab shows: the account's, with the
// survey's own parts laid over it. A header or a footer turned off is
// still shown, so that turning it back on finds what was there.
func (d Draft) FormStyle(account Style) Style {
	own := d.OwnParts()
	s := account
	if own.Theme {
		s.Theme = d.Style.Theme
	}
	if own.Header {
		s.Header = d.Style.Header
	}
	if own.Footer {
		s.Footer = d.Style.Footer
	}
	if own.Thanks {
		s.Thanks = d.Style.Thanks
	}
	return s
}

// ResolvedStyle is the style the survey's pages are drawn in: FormStyle
// with a header or a footer the survey turned off taken away. The logo
// is part of the header, so a survey with no header carries no mark of
// its own and Earful's owl returns to its footer.
func (d Draft) ResolvedStyle(account Style) Style {
	s := d.FormStyle(account)
	if d.StyleChoice.NoHeader {
		s.Header = StyleHeader{}
	}
	if d.StyleChoice.NoFooter {
		s.Footer = StyleFooter{}
	}
	return s
}

// SetStyleFromForm records what the Style tab posted. submitted is the
// whole style as the form shows it, with the parts it carried changed;
// carried names those parts. A carried part that is the account's own
// part as it stands is followed, and one that differs becomes the
// survey's own, whole: a header is one statement of who is asking, so
// changing its tagline makes the whole header the survey's. header and
// footer, where not nil, say whether the survey shows a header and a
// footer at all.
//
// Words the survey takes over from its account, unchanged, keep the
// account's translations, so a part made the survey's own only to change
// its name is not translated again.
func (d *Draft) SetStyleFromForm(account WorkspaceStyle, submitted Style, carried StyleParts, header, footer *bool) error {
	submitted.Thanks = submitted.Thanks.settled()
	if submitted.Theme == ThemeEarful {
		submitted.Theme = ""
	}
	if err := submitted.Validate(); err != nil {
		return err
	}
	before := d.OwnParts()
	ws := account.Style
	if carried.Theme {
		if SameTheme(submitted.Theme, ws.Theme) {
			d.Style.Theme, d.StyleChoice.Own.Theme = "", false
		} else {
			d.Style.Theme, d.StyleChoice.Own.Theme = submitted.Theme, true
		}
	}
	if carried.Header {
		if submitted.Header.Equal(ws.Header) {
			d.Style.Header, d.StyleChoice.Own.Header = StyleHeader{}, false
		} else {
			d.Style.Header, d.StyleChoice.Own.Header = submitted.Header, true
		}
	}
	if carried.Footer {
		if submitted.Footer.Equal(ws.Footer) {
			d.Style.Footer, d.StyleChoice.Own.Footer = StyleFooter{}, false
		} else {
			d.Style.Footer, d.StyleChoice.Own.Footer = submitted.Footer, true
		}
	}
	if carried.Thanks {
		if submitted.Thanks.Equal(ws.Thanks) {
			d.Style.Thanks, d.StyleChoice.Own.Thanks = StyleThanks{}, false
		} else {
			d.Style.Thanks, d.StyleChoice.Own.Thanks = submitted.Thanks, true
		}
	}
	if header != nil {
		d.StyleChoice.NoHeader = !*header
	}
	if footer != nil {
		d.StyleChoice.NoFooter = !*footer
	}
	after := d.OwnParts()
	d.adoptStyleTranslations(account, StyleParts{
		Header: after.Header && !before.Header,
		Footer: after.Footer && !before.Footer,
		Thanks: after.Thanks && !before.Thanks,
	})
	return nil
}

// ResetStyle makes the survey follow every part of its account's style
// again, with its header and footer shown. Its own parts are let go.
func (d *Draft) ResetStyle() {
	d.Style = Style{}
	d.StyleChoice = StyleChoice{}
}

// adoptStyleTranslations gives the parts the survey has just made its
// own the account's translations of their words, where the survey's
// words for a part are still the account's. They count as read only
// where the account's were read against its current wording and the
// survey's own translation, if it had one, was read too.
func (d *Draft) adoptStyleTranslations(account WorkspaceStyle, parts StyleParts) {
	if parts.IsZero() || len(d.Localizations) == 0 {
		return
	}
	accountWords := account.Style.Words()
	ownWords := d.Style.Words()
	var same StyleParts
	for _, p := range []StyleParts{{Header: true}, {Footer: true}, {Thanks: true}} {
		if !partsWithin(p, parts) {
			continue
		}
		words := ownWords.Only(p)
		if words.IsZero() || !words.Equal(accountWords.Only(p)) {
			continue
		}
		same = unionParts(same, p)
	}
	if same.IsZero() {
		return
	}
	for _, lang := range d.Languages() {
		translated, ok := account.Localizations[lang]
		if !ok {
			continue
		}
		localization := d.Localizations[lang]
		var t LocalizedStyle
		if localization.Style != nil {
			t = *localization.Style
		} else {
			t.Reviewed = true
		}
		t.StyleWords = t.StyleWords.With(translated.StyleWords, same)
		t.Source = t.Source.With(ownWords, same)
		t.Reviewed = t.Reviewed && !account.PendingFor(lang, same)
		localization.Style = &t
		d.Localizations[lang] = localization
	}
}

func partsWithin(p, of StyleParts) bool {
	return (!p.Theme || of.Theme) && (!p.Header || of.Header) && (!p.Footer || of.Footer) && (!p.Thanks || of.Thanks)
}

func unionParts(a, b StyleParts) StyleParts {
	return StyleParts{Theme: a.Theme || b.Theme, Header: a.Header || b.Header, Footer: a.Footer || b.Footer, Thanks: a.Thanks || b.Thanks}
}

// ownWordParts are the parts whose words the survey translates itself:
// its own parts with words, less a header or a footer it does not show.
func (d Draft) ownWordParts() StyleParts {
	own := d.OwnParts()
	own.Theme = false
	if d.StyleChoice.NoHeader {
		own.Header = false
	}
	if d.StyleChoice.NoFooter {
		own.Footer = false
	}
	return own
}

// followedWordParts are the parts whose words the survey shows from its
// account, translated on the account.
func (d Draft) followedWordParts() StyleParts {
	f := d.FollowedParts()
	f.Theme = false
	return f
}

// OwnStyleWords are the words of the survey's own parts that its pages
// show: the words the survey's Languages tab translates. The words it
// follows are translated on the account.
func (d Draft) OwnStyleWords() StyleWords { return d.Style.Words().Only(d.ownWordParts()) }

// FollowsAccountWords reports whether the survey shows words of its
// account's style, which the account translates for it.
func (d Draft) FollowsAccountWords(account WorkspaceStyle) bool {
	return !account.Style.Words().Only(d.followedWordParts()).IsZero()
}

// AccountStylePending reports whether the account's translation of the
// words the survey shows from it still needs translating or reviewing in
// lang.
func (d Draft) AccountStylePending(account WorkspaceStyle, lang string) bool {
	return account.PendingFor(lang, d.followedWordParts())
}

// AccountStyleTranslationError refuses to publish a survey in a language
// the account's style has not been translated into, or whose translation
// has not been read against its current wording, where the survey shows
// words of the account's style.
type AccountStyleTranslationError struct {
	Lang string
}

func (e AccountStyleTranslationError) Error() string {
	return "the account style is not reviewed in " + e.Lang
}

// IsAccountStyleTranslationError reports whether err is one, and which
// language it names.
func IsAccountStyleTranslationError(err error) (string, bool) {
	var e AccountStyleTranslationError
	if errors.As(err, &e) {
		return e.Lang, true
	}
	return "", false
}

// ResolvedStyleWords are the words of the resolved style in lang: the
// survey's own translation for its own parts and the account's for the
// parts it follows. It reports false where there is nothing to translate,
// or where either translation still needs reading, which is what keeps a
// language out of a published version until it has been read.
func (d Draft) ResolvedStyleWords(account WorkspaceStyle, lang string) (StyleWords, bool) {
	lang = NormalizeLang(lang)
	own, followed := d.ownWordParts(), d.followedWordParts()
	ownSource := d.OwnStyleWords()
	followedSource := account.Style.Words().Only(followed)
	if ownSource.IsZero() && followedSource.IsZero() {
		return StyleWords{}, false
	}
	var words StyleWords
	if !ownSource.IsZero() {
		translated := d.Localizations[lang].Style
		if translated == nil || d.StylePending(lang) {
			return StyleWords{}, false
		}
		words = translated.StyleWords.Only(own)
	}
	if !followedSource.IsZero() {
		if account.PendingFor(lang, followed) {
			return StyleWords{}, false
		}
		words = words.With(account.Localizations[lang].StyleWords, followed)
	}
	return words, true
}

// ResolvedLocalizedStyle is the resolved style as somebody reading lang
// sees it, where both its translations are read.
func (d Draft) ResolvedLocalizedStyle(account WorkspaceStyle, lang string) (Style, bool) {
	words, ok := d.ResolvedStyleWords(account, lang)
	if !ok {
		return Style{}, false
	}
	return d.ResolvedStyle(account.Style).WithWords(words), true
}

// StyleDiffers reports which parts of two styles draw different pages.
func StyleDiffers(a, b Style) StyleParts {
	return StyleParts{
		Theme:  !SameTheme(a.Theme, b.Theme),
		Header: !a.Header.Equal(b.Header),
		Footer: !a.Footer.Equal(b.Footer),
		Thanks: !a.Thanks.Equal(b.Thanks),
	}
}
