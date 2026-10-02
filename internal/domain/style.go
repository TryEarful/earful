package domain

import (
	"errors"
	"slices"
	"strings"
)

// Style is how a survey looks to the people answering it (ADR-0018): the
// theme its pages are drawn in, the header above the survey and the
// footer below it. It is part of the draft and is frozen into each
// published version with the questions, because the look of the page is
// part of what a respondent was shown (ADR-0001). The zero value means
// "not set": the survey is drawn as Earful draws every page, exactly as
// it was before a creator could choose.
//
// It is a struct of its own, and not a field or two on Draft, so that
// the rest of a survey's style (ADR-0018: a banner, a logo, a picture
// for the thanks page) joins what is here and is frozen by the same
// column.
type Style struct {
	// Theme names one of the themes in web/static/css/app.css. It is
	// empty for Earful's own, so a draft that never chose one and a
	// draft that chose the default are the same draft.
	Theme string `json:"theme,omitempty"`
	// Header is what stands above the survey: whose it is.
	Header StyleHeader `json:"header,omitzero"`
	// Footer is what stands below it, above Earful's own footer.
	Footer StyleFooter `json:"footer,omitzero"`
}

// StyleHeader is the head of a respondent's page: the name of whoever
// the survey is from, a line or two about them, and links. Every part is
// plain text and optional.
type StyleHeader struct {
	Name    string      `json:"name,omitempty"`
	Tagline string      `json:"tagline,omitempty"`
	Links   []StyleLink `json:"links,omitempty"`
}

// StyleFooter is the creator's own footer: a text, such as a legal name
// and an address, and links.
type StyleFooter struct {
	Text  string      `json:"text,omitempty"`
	Links []StyleLink `json:"links,omitempty"`
}

// StyleLink is a link in a header or a footer. It always has both a
// label and an address: a bare address tells a respondent nothing about
// where it goes, and a label with no address goes nowhere.
type StyleLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// The themes a survey can be drawn in. ThemeEarful is the default and is
// stored as the empty string.
const (
	ThemeEarful = "earful"
	ThemeSlate  = "slate"
	ThemeOcean  = "ocean"
	ThemeForest = "forest"
)

const (
	maxStyleNameLen = 80
	// The tagline and the footer text are a couple of sentences, not a
	// page: the header must leave the survey in view on a phone.
	maxStyleTaglineLen = 280
	maxStyleFooterLen  = 280
	// MaxStyleLinks is how many links a header, and a footer, can hold.
	// The Style tab draws this many rows for each.
	MaxStyleLinks = 3
)

// Themes lists every theme, the default first, in the order they are
// offered.
func Themes() []string {
	return []string{ThemeEarful, ThemeSlate, ThemeOcean, ThemeForest}
}

// ErrUnknownTheme refuses a theme the stylesheet does not have. The name
// becomes a class on the respondent's page, so only a known one may be
// stored.
var ErrUnknownTheme = errors.New("choose one of the themes offered")

// StylePart names a part of a style, so that a problem can be shown
// beside the field it is in.
type StylePart string

const (
	StyleTheme       StylePart = "theme"
	StyleHeaderName  StylePart = "header_name"
	StyleHeaderText  StylePart = "header_tagline"
	StyleHeaderLinks StylePart = "header_links"
	StyleFooterText  StylePart = "footer_text"
	StyleFooterLinks StylePart = "footer_links"
)

// StyleError says which part of a style a problem belongs to. Link
// counts from one, and is zero where the problem is not one link's.
type StyleError struct {
	Part StylePart
	Link int
	Err  error
}

func (e StyleError) Error() string { return string(e.Part) + ": " + e.Err.Error() }

func (e StyleError) Unwrap() error { return e.Err }

// NewStyle builds a Style with a theme and nothing else, and checks it.
// The default theme is stored as the empty string.
func NewStyle(theme string) (Style, error) {
	return Style{}.WithTheme(theme)
}

// WithTheme returns the style drawn in another theme, with everything
// else as it was.
func (s Style) WithTheme(theme string) (Style, error) {
	theme = strings.TrimSpace(theme)
	if theme == ThemeEarful {
		theme = ""
	}
	if !knownTheme(theme) {
		return s, StyleError{Part: StyleTheme, Err: ErrUnknownTheme}
	}
	s.Theme = theme
	return s, nil
}

// NewStyleHeader builds a header from what a creator typed: trimmed,
// with line endings made uniform, and with the link rows left empty
// dropped. It is returned as typed even when it is refused, so the form
// can show it again.
func NewStyleHeader(name, tagline string, links []StyleLink) (StyleHeader, error) {
	h := StyleHeader{
		Name:    strings.Join(strings.Fields(name), " "),
		Tagline: normalizeMessage(tagline),
		Links:   cleanLinks(links),
	}
	return h, h.validate()
}

// NewStyleFooter builds a footer from what a creator typed, as
// NewStyleHeader builds a header.
func NewStyleFooter(text string, links []StyleLink) (StyleFooter, error) {
	f := StyleFooter{Text: normalizeMessage(text), Links: cleanLinks(links)}
	return f, f.validate()
}

// cleanLinks trims every row and drops the rows with nothing in them,
// so that the second of three rows can be filled and the first left
// empty.
func cleanLinks(links []StyleLink) []StyleLink {
	var out []StyleLink
	for _, link := range links {
		link.Label = strings.TrimSpace(link.Label)
		link.URL = strings.TrimSpace(link.URL)
		if link.Label == "" && link.URL == "" {
			continue
		}
		out = append(out, link)
	}
	return out
}

// IsZero reports whether the header shows nothing.
func (h StyleHeader) IsZero() bool {
	return h.Name == "" && h.Tagline == "" && len(h.Links) == 0
}

// IsZero reports whether the footer shows nothing.
func (f StyleFooter) IsZero() bool { return f.Text == "" && len(f.Links) == 0 }

// IsZero reports whether nothing is set, so the survey is drawn in the
// default style.
func (s Style) IsZero() bool {
	return s.Theme == "" && s.Header.IsZero() && s.Footer.IsZero()
}

// Equal reports whether two styles would draw the same pages.
func (s Style) Equal(other Style) bool {
	return s.Theme == other.Theme &&
		s.Header.Name == other.Header.Name &&
		s.Header.Tagline == other.Header.Tagline &&
		slices.Equal(s.Header.Links, other.Header.Links) &&
		s.Footer.Text == other.Footer.Text &&
		slices.Equal(s.Footer.Links, other.Footer.Links)
}

// ThemeName is the theme the survey is drawn in, by name: the stored
// one, or the default where none was chosen.
func (s Style) ThemeName() string {
	if s.Theme == "" {
		return ThemeEarful
	}
	return s.Theme
}

// TaglineLines is the tagline as its writer laid it out, a line at a
// time. The text is never interpreted, so a creator cannot put markup on
// a respondent's page.
func (h StyleHeader) TaglineLines() []string { return lines(h.Tagline) }

// TextLines is the footer's text a line at a time, which is how an
// address is written.
func (f StyleFooter) TextLines() []string { return lines(f.Text) }

func lines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func knownTheme(theme string) bool {
	switch theme {
	case "", ThemeSlate, ThemeOcean, ThemeForest:
		return true
	}
	return false
}

// Validate checks the theme, the lengths, and every link. The first
// problem is returned as a StyleError naming its part.
func (s Style) Validate() error {
	if !knownTheme(s.Theme) {
		return StyleError{Part: StyleTheme, Err: ErrUnknownTheme}
	}
	if err := s.Header.validate(); err != nil {
		return err
	}
	return s.Footer.validate()
}

func (h StyleHeader) validate() error {
	if len([]rune(h.Name)) > maxStyleNameLen {
		return StyleError{Part: StyleHeaderName, Err: LimitError{Kind: LimitStyleName, Limit: maxStyleNameLen}}
	}
	if len([]rune(h.Tagline)) > maxStyleTaglineLen {
		return StyleError{Part: StyleHeaderText, Err: LimitError{Kind: LimitStyleTagline, Limit: maxStyleTaglineLen}}
	}
	return validateLinks(StyleHeaderLinks, h.Links)
}

func (f StyleFooter) validate() error {
	if len([]rune(f.Text)) > maxStyleFooterLen {
		return StyleError{Part: StyleFooterText, Err: LimitError{Kind: LimitStyleFooter, Limit: maxStyleFooterLen}}
	}
	return validateLinks(StyleFooterLinks, f.Links)
}

// validateLinks applies to each link the rules the thank you page's link
// follows: a label, and an absolute http or https address.
func validateLinks(part StylePart, links []StyleLink) error {
	if len(links) > MaxStyleLinks {
		return StyleError{Part: part, Err: LimitError{Kind: LimitStyleLinks, Limit: MaxStyleLinks}}
	}
	for i, link := range links {
		var err error
		switch {
		case len([]rune(link.Label)) > maxThanksLabelLen:
			err = LimitError{Kind: LimitThanksLabel, Limit: maxThanksLabelLen}
		case link.URL == "":
			err = ErrThanksLinkAddress
		case link.Label == "":
			err = ErrThanksLinkLabel
		default:
			err = ValidateLinkURL(link.URL)
		}
		if err != nil {
			return StyleError{Part: part, Link: i + 1, Err: err}
		}
	}
	return nil
}

// SetStyle replaces the draft's style.
func (d *Draft) SetStyle(s Style) error {
	if err := s.Validate(); err != nil {
		return err
	}
	d.Style = s
	return nil
}

// --- the words of a style, and their translations ---------------------------

// StyleWords are the parts of a style that are written in a language and
// so are translated: the tagline, the footer's text and every link's
// label, by the link's position. The name is a proper noun and the
// addresses are the same pages in every language; both are shared.
type StyleWords struct {
	Tagline     string   `json:"tagline,omitempty"`
	HeaderLinks []string `json:"header_links,omitempty"`
	FooterText  string   `json:"footer_text,omitempty"`
	FooterLinks []string `json:"footer_links,omitempty"`
}

// Words are the style's translatable words as the creator wrote them.
func (s Style) Words() StyleWords {
	return StyleWords{
		Tagline:     s.Header.Tagline,
		HeaderLinks: linkLabels(s.Header.Links),
		FooterText:  s.Footer.Text,
		FooterLinks: linkLabels(s.Footer.Links),
	}
}

func linkLabels(links []StyleLink) []string {
	if len(links) == 0 {
		return nil
	}
	labels := make([]string, len(links))
	for i, link := range links {
		labels[i] = link.Label
	}
	return labels
}

// IsZero reports whether there are no words.
func (w StyleWords) IsZero() bool {
	return w.Tagline == "" && w.FooterText == "" && len(w.HeaderLinks) == 0 && len(w.FooterLinks) == 0
}

// Equal reports whether two sets of words are the same words.
func (w StyleWords) Equal(other StyleWords) bool {
	return w.Tagline == other.Tagline && w.FooterText == other.FooterText &&
		slices.Equal(w.HeaderLinks, other.HeaderLinks) &&
		slices.Equal(w.FooterLinks, other.FooterLinks)
}

// covers reports whether w has a word for every word of source.
func (w StyleWords) covers(source StyleWords) bool {
	if source.Tagline != "" && w.Tagline == "" {
		return false
	}
	if source.FooterText != "" && w.FooterText == "" {
		return false
	}
	return labelsCover(w.HeaderLinks, source.HeaderLinks) && labelsCover(w.FooterLinks, source.FooterLinks)
}

func labelsCover(labels, source []string) bool {
	if len(labels) != len(source) {
		return false
	}
	return !slices.Contains(labels, "")
}

// WithWords returns the style worded in another language: the tagline,
// the footer's text and the labels replaced, the name and the addresses
// kept. A word the translation lacks stays as written.
func (s Style) WithWords(w StyleWords) Style {
	if w.Tagline != "" {
		s.Header.Tagline = w.Tagline
	}
	if w.FooterText != "" {
		s.Footer.Text = w.FooterText
	}
	s.Header.Links = relabel(s.Header.Links, w.HeaderLinks)
	s.Footer.Links = relabel(s.Footer.Links, w.FooterLinks)
	return s
}

func relabel(links []StyleLink, labels []string) []StyleLink {
	if len(links) == 0 {
		return links
	}
	out := slices.Clone(links)
	for i := range out {
		if i < len(labels) && labels[i] != "" {
			out[i].Label = labels[i]
		}
	}
	return out
}

// LocalizedStyle is a style's words in one language. Source is the
// wording the translation was made from, which is how a change in the
// source is detected.
type LocalizedStyle struct {
	StyleWords
	Reviewed bool       `json:"reviewed"`
	Source   StyleWords `json:"source,omitzero"`
}

// HasStyleToTranslate reports whether the style carries any words of the
// creator's that a language would word differently.
func (d Draft) HasStyleToTranslate() bool { return !d.Style.Words().IsZero() }

// StylePending reports whether a language still needs the style's words
// translated or reviewed: never translated, not yet read, missing a part
// the source has, or made from a wording since changed.
func (d Draft) StylePending(lang string) bool {
	localization, ok := d.Localizations[NormalizeLang(lang)]
	if !ok || !d.HasStyleToTranslate() {
		return false
	}
	translated := localization.Style
	words := d.Style.Words()
	return translated == nil || !translated.Reviewed ||
		!translated.Source.Equal(words) || !translated.covers(words)
}

// StyleStale reports whether a language's style was translated from a
// wording the creator has since changed.
func (d Draft) StyleStale(lang string) bool {
	localization, ok := d.Localizations[NormalizeLang(lang)]
	if !ok || localization.Style == nil {
		return false
	}
	return !localization.Style.IsZero() && !localization.Style.Source.Equal(d.Style.Words())
}

// SetStyleTranslation records the style's words in one language, as read
// by the creator when reviewed is true. A part the source does not have
// is dropped, so a translation never shows more than the original.
func (d *Draft) SetStyleTranslation(lang string, words StyleWords, reviewed bool) error {
	lang = NormalizeLang(lang)
	localization, ok := d.Localizations[lang]
	if !ok {
		return ErrUnknownLanguage
	}
	source := d.Style.Words()
	t := LocalizedStyle{
		StyleWords: StyleWords{
			Tagline:     normalizeMessage(words.Tagline),
			HeaderLinks: fitLabels(words.HeaderLinks, len(source.HeaderLinks)),
			FooterText:  normalizeMessage(words.FooterText),
			FooterLinks: fitLabels(words.FooterLinks, len(source.FooterLinks)),
		},
		Reviewed: reviewed,
		Source:   source,
	}
	if source.Tagline == "" {
		t.Tagline = ""
	}
	if source.FooterText == "" {
		t.FooterText = ""
	}
	if len([]rune(t.Tagline)) > maxStyleTaglineLen {
		return StyleError{Part: StyleHeaderText, Err: LimitError{Kind: LimitStyleTagline, Limit: maxStyleTaglineLen}}
	}
	if len([]rune(t.FooterText)) > maxStyleFooterLen {
		return StyleError{Part: StyleFooterText, Err: LimitError{Kind: LimitStyleFooter, Limit: maxStyleFooterLen}}
	}
	for _, label := range append(slices.Clone(t.HeaderLinks), t.FooterLinks...) {
		if len([]rune(label)) > maxThanksLabelLen {
			return LimitError{Kind: LimitThanksLabel, Limit: maxThanksLabelLen}
		}
	}
	localization.Style = &t
	d.Localizations[lang] = localization
	return nil
}

// fitLabels trims the labels and makes them as many as the source's
// links, so that a label always answers to the link in its position.
func fitLabels(labels []string, n int) []string {
	if n == 0 {
		return nil
	}
	out := make([]string, n)
	for i := range out {
		if i < len(labels) {
			out[i] = strings.TrimSpace(labels[i])
		}
	}
	return out
}

// LocalizedStyle returns the style as a respondent reading lang sees it:
// the translated words with the creator's theme, name and addresses. It
// is what publishing freezes, so it reports false until the language is
// reviewed against the current wording.
func (d Draft) LocalizedStyle(lang string) (Style, bool) {
	lang = NormalizeLang(lang)
	if !d.HasStyleToTranslate() || d.StylePending(lang) {
		return Style{}, false
	}
	translated := d.Localizations[lang].Style
	if translated == nil {
		return Style{}, false
	}
	return d.Style.WithWords(translated.StyleWords), true
}
