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
	// Thanks is the picture on the page that thanks a respondent.
	Thanks StyleThanks `json:"thanks,omitzero"`
}

// StyleThanks is the picture above the thanks page's heading: Earful's
// happy owl, one of the drawings Earful offers, a picture of the
// creator's own, or none. The zero value is the owl, which is what every
// thanks page showed before a creator could choose.
type StyleThanks struct {
	// Picture names the choice, and is empty for the owl.
	Picture string `json:"picture,omitempty"`
	// Image and Alt are the creator's own picture and what it says to
	// somebody who cannot see it. They are set only while Picture is
	// ThanksImage: a picture the page does not show is not kept.
	Image StyleImage `json:"image,omitzero"`
	Alt   string     `json:"alt,omitempty"`
}

// The pictures a thanks page can show. ThanksOwl is the default and is
// stored as the empty string. The three drawings have no owl in them, so
// a survey that carries its own mark can thank in a way that is not
// Earful's.
const (
	ThanksOwl      = "owl"
	ThanksCheck    = "check"
	ThanksEnvelope = "envelope"
	ThanksConfetti = "confetti"
	ThanksImage    = "image"
	ThanksNone     = "none"
)

// ThanksPictures lists every choice, the default first, in the order they
// are offered.
func ThanksPictures() []string {
	return []string{ThanksOwl, ThanksCheck, ThanksEnvelope, ThanksConfetti, ThanksImage, ThanksNone}
}

// PictureName is the choice by name: the stored one, or the owl where
// none was made.
func (t StyleThanks) PictureName() string {
	if t.Picture == "" {
		return ThanksOwl
	}
	return t.Picture
}

// IsZero reports whether the thanks page shows the owl, as it always did.
func (t StyleThanks) IsZero() bool { return t.Picture == "" && t.Image.IsZero() && t.Alt == "" }

// Drawing reports whether the choice is one of the drawings Earful
// offers.
func (t StyleThanks) Drawing() bool {
	switch t.Picture {
	case ThanksCheck, ThanksEnvelope, ThanksConfetti:
		return true
	}
	return false
}

// WithPicture returns the choice changed, with the creator's picture
// kept for now: the form that changes the choice may also bring the
// picture, and the two are settled together when the style is set.
func (t StyleThanks) WithPicture(picture string) (StyleThanks, error) {
	picture = strings.TrimSpace(picture)
	if picture == ThanksOwl {
		picture = ""
	}
	if !knownThanksPicture(picture) {
		return t, StyleError{Part: StyleThanksPicture, Err: ErrUnknownThanksPicture}
	}
	t.Picture = picture
	return t, nil
}

// WithAlt returns the choice with the alternative text as typed, trimmed
// to one line.
func (t StyleThanks) WithAlt(alt string) StyleThanks {
	t.Alt = strings.Join(strings.Fields(alt), " ")
	return t
}

// settled drops the creator's picture where the choice is another one,
// so that a style refers only to pictures its pages show.
func (t StyleThanks) settled() StyleThanks {
	if t.Picture != ThanksImage {
		t.Image, t.Alt = StyleImage{}, ""
	}
	return t
}

func knownThanksPicture(picture string) bool {
	switch picture {
	case "", ThanksCheck, ThanksEnvelope, ThanksConfetti, ThanksImage, ThanksNone:
		return true
	}
	return false
}

func (t StyleThanks) validate() error {
	if !knownThanksPicture(t.Picture) {
		return StyleError{Part: StyleThanksPicture, Err: ErrUnknownThanksPicture}
	}
	if err := t.Image.validate(); err != nil {
		return StyleError{Part: StyleThanksImage, Err: err}
	}
	if len([]rune(t.Alt)) > maxStyleThanksAltLen {
		return StyleError{Part: StyleThanksAlt, Err: LimitError{Kind: LimitStyleThanksAlt, Limit: maxStyleThanksAltLen}}
	}
	if t.Picture == ThanksImage && t.Image.IsZero() {
		return StyleError{Part: StyleThanksImage, Err: ErrThanksImageMissing}
	}
	if !t.Image.IsZero() && t.Alt == "" {
		return StyleError{Part: StyleThanksAlt, Err: ErrThanksImageAlt}
	}
	return nil
}

// StyleHeader is the head of a respondent's page: a banner, the logo and
// the name of whoever the survey is from, a line or two about them, and
// links. Every part is optional, and the words are plain text.
type StyleHeader struct {
	Name    string      `json:"name,omitempty"`
	Tagline string      `json:"tagline,omitempty"`
	Links   []StyleLink `json:"links,omitempty"`
	// Banner is the strip across the head of the page. It is decoration:
	// nothing is written on it and it has no alternative text.
	Banner StyleImage `json:"banner,omitzero"`
	// Logo is the survey's own mark, and LogoAlt what it says to
	// somebody who cannot see it. A logo always has one.
	Logo    StyleImage `json:"logo,omitzero"`
	LogoAlt string     `json:"logo_alt,omitempty"`
}

// StyleImage refers to a picture stored beside the survey
// (survey_images) by the hash of its bytes, which is also its address.
// The size travels with it so that a page can leave the picture's room
// before the picture arrives. The zero value is "no picture".
type StyleImage struct {
	SHA256 string `json:"sha256"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// IsZero reports whether there is no picture.
func (i StyleImage) IsZero() bool { return i.SHA256 == "" }

// validate checks that the reference could be a stored picture's: the
// hash becomes part of an address, so only a hash may be stored.
func (i StyleImage) validate() error {
	if i.IsZero() {
		return nil
	}
	if len(i.SHA256) != 64 || strings.Trim(i.SHA256, "0123456789abcdef") != "" || i.Width <= 0 || i.Height <= 0 {
		return ErrStyleImage
	}
	return nil
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
	// A logo's alternative text is a name, not a description of a
	// drawing.
	maxStyleLogoAltLen = 120
	// A thanks picture's alternative text says what it shows in a line.
	maxStyleThanksAltLen = 120
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

var (
	// ErrStyleLogoAlt: a logo with no alternative text is a blank to
	// somebody using a screen reader, on the one page element that says
	// whose survey this is.
	ErrStyleLogoAlt = errors.New("say what the logo shows, for people who cannot see it")
	// ErrStyleImage: a picture reference that is not a stored picture's.
	// A form cannot produce one; it is refused all the same.
	ErrStyleImage = errors.New("that image cannot be used")
	// ErrUnknownThanksPicture refuses a thanks picture that is not one of
	// the choices offered. The name picks what the page draws, so only a
	// known one may be stored.
	ErrUnknownThanksPicture = errors.New("choose one of the pictures offered")
	// ErrThanksImageMissing: "your own picture" was chosen with no
	// picture uploaded, or with the one there removed.
	ErrThanksImageMissing = errors.New("choose an image to upload, or another picture")
	// ErrThanksImageAlt: the creator's own picture says nothing to
	// somebody using a screen reader without it.
	ErrThanksImageAlt = errors.New("say what the picture shows, for people who cannot see it")
)

// StylePart names a part of a style, so that a problem can be shown
// beside the field it is in.
type StylePart string

const (
	StyleTheme       StylePart = "theme"
	StyleHeaderName  StylePart = "header_name"
	StyleHeaderText  StylePart = "header_tagline"
	StyleHeaderLinks StylePart = "header_links"
	StyleBanner      StylePart = "banner"
	StyleLogo        StylePart = "logo"
	StyleLogoAlt     StylePart = "logo_alt"
	StyleFooterText  StylePart = "footer_text"
	StyleFooterLinks StylePart = "footer_links"
	// The thanks page's picture: the choice, the creator's own picture,
	// and its alternative text.
	StyleThanksPicture StylePart = "thanks_picture"
	StyleThanksImage   StylePart = "thanks_image"
	StyleThanksAlt     StylePart = "thanks_alt"
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

// WithWords returns the header with its name, tagline and links replaced
// and its pictures kept: the words and the pictures arrive in different
// fields of the form, and one never clears the other.
func (h StyleHeader) WithWords(words StyleHeader) StyleHeader {
	h.Name, h.Tagline, h.Links = words.Name, words.Tagline, words.Links
	return h
}

// WithLogoAlt returns the header with the logo's alternative text as
// typed, trimmed to one line.
func (h StyleHeader) WithLogoAlt(alt string) StyleHeader {
	h.LogoAlt = strings.Join(strings.Fields(alt), " ")
	return h
}

// HasImages reports whether the header draws a picture.
func (h StyleHeader) HasImages() bool { return !h.Banner.IsZero() || !h.Logo.IsZero() }

// Images lists the pictures a style refers to, so that the store can
// tell which stored pictures are in use.
func (s Style) Images() []StyleImage {
	var out []StyleImage
	for _, image := range []StyleImage{s.Header.Banner, s.Header.Logo, s.Thanks.Image} {
		if !image.IsZero() {
			out = append(out, image)
		}
	}
	return out
}

// Branded reports whether the survey carries its own mark. A page has
// one mark: where the survey has a logo, Earful's owl leaves the footer
// (ADR-0018).
func (s Style) Branded() bool { return !s.Header.Logo.IsZero() }

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
	return h.Name == "" && h.Tagline == "" && len(h.Links) == 0 && !h.HasImages()
}

// IsZero reports whether the footer shows nothing.
func (f StyleFooter) IsZero() bool { return f.Text == "" && len(f.Links) == 0 }

// IsZero reports whether nothing is set, so the survey is drawn in the
// default style.
func (s Style) IsZero() bool {
	return s.Theme == "" && s.Header.IsZero() && s.Footer.IsZero() && s.Thanks.IsZero()
}

// Equal reports whether two styles would draw the same pages.
func (s Style) Equal(other Style) bool {
	return s.Theme == other.Theme &&
		s.Header.Name == other.Header.Name &&
		s.Header.Tagline == other.Header.Tagline &&
		slices.Equal(s.Header.Links, other.Header.Links) &&
		s.Header.Banner == other.Header.Banner &&
		s.Header.Logo == other.Header.Logo &&
		s.Header.LogoAlt == other.Header.LogoAlt &&
		s.Footer.Text == other.Footer.Text &&
		slices.Equal(s.Footer.Links, other.Footer.Links) &&
		s.Thanks == other.Thanks
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
	if err := s.Footer.validate(); err != nil {
		return err
	}
	return s.Thanks.validate()
}

func (h StyleHeader) validate() error {
	if len([]rune(h.Name)) > maxStyleNameLen {
		return StyleError{Part: StyleHeaderName, Err: LimitError{Kind: LimitStyleName, Limit: maxStyleNameLen}}
	}
	if len([]rune(h.Tagline)) > maxStyleTaglineLen {
		return StyleError{Part: StyleHeaderText, Err: LimitError{Kind: LimitStyleTagline, Limit: maxStyleTaglineLen}}
	}
	if err := validateLinks(StyleHeaderLinks, h.Links); err != nil {
		return err
	}
	if err := h.Banner.validate(); err != nil {
		return StyleError{Part: StyleBanner, Err: err}
	}
	if err := h.Logo.validate(); err != nil {
		return StyleError{Part: StyleLogo, Err: err}
	}
	if len([]rune(h.LogoAlt)) > maxStyleLogoAltLen {
		return StyleError{Part: StyleLogoAlt, Err: LimitError{Kind: LimitStyleLogoAlt, Limit: maxStyleLogoAltLen}}
	}
	if !h.Logo.IsZero() && h.LogoAlt == "" {
		return StyleError{Part: StyleLogoAlt, Err: ErrStyleLogoAlt}
	}
	return nil
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

// SetStyle replaces the draft's style. A picture of the creator's left
// behind by a change of the thanks page's choice is dropped first, so
// the draft refers only to pictures its pages show.
func (d *Draft) SetStyle(s Style) error {
	s.Thanks = s.Thanks.settled()
	if err := s.Validate(); err != nil {
		return err
	}
	d.Style = s
	return nil
}

// --- the words of a style, and their translations ---------------------------

// StyleWords are the parts of a style that are written in a language and
// so are translated: the tagline, the logo's alternative text, the
// footer's text, every link's label, by the link's position, and the
// alternative text of the thanks page's own picture. The name
// is a proper noun and the addresses are the same pages in every
// language; both are shared.
type StyleWords struct {
	Tagline     string   `json:"tagline,omitempty"`
	LogoAlt     string   `json:"logo_alt,omitempty"`
	HeaderLinks []string `json:"header_links,omitempty"`
	FooterText  string   `json:"footer_text,omitempty"`
	FooterLinks []string `json:"footer_links,omitempty"`
	ThanksAlt   string   `json:"thanks_alt,omitempty"`
}

// Words are the style's translatable words as the creator wrote them.
func (s Style) Words() StyleWords {
	return StyleWords{
		Tagline:     s.Header.Tagline,
		LogoAlt:     s.logoAlt(),
		HeaderLinks: linkLabels(s.Header.Links),
		FooterText:  s.Footer.Text,
		FooterLinks: linkLabels(s.Footer.Links),
		ThanksAlt:   s.thanksAlt(),
	}
}

// thanksAlt is the alternative text of the thanks page's own picture,
// where the page shows one.
func (s Style) thanksAlt() string {
	if s.Thanks.Image.IsZero() {
		return ""
	}
	return s.Thanks.Alt
}

// logoAlt is the alternative text of the logo the style has. Text left
// in the field after the logo was removed is nobody's to translate.
func (s Style) logoAlt() string {
	if s.Header.Logo.IsZero() {
		return ""
	}
	return s.Header.LogoAlt
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
	return w.Tagline == "" && w.LogoAlt == "" && w.FooterText == "" && w.ThanksAlt == "" &&
		len(w.HeaderLinks) == 0 && len(w.FooterLinks) == 0
}

// Equal reports whether two sets of words are the same words.
func (w StyleWords) Equal(other StyleWords) bool {
	return w.Tagline == other.Tagline && w.LogoAlt == other.LogoAlt && w.FooterText == other.FooterText &&
		w.ThanksAlt == other.ThanksAlt &&
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
	if source.LogoAlt != "" && w.LogoAlt == "" {
		return false
	}
	if source.ThanksAlt != "" && w.ThanksAlt == "" {
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
	if w.LogoAlt != "" && !s.Header.Logo.IsZero() {
		s.Header.LogoAlt = w.LogoAlt
	}
	if w.ThanksAlt != "" && !s.Thanks.Image.IsZero() {
		s.Thanks.Alt = w.ThanksAlt
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
			LogoAlt:     strings.Join(strings.Fields(words.LogoAlt), " "),
			HeaderLinks: fitLabels(words.HeaderLinks, len(source.HeaderLinks)),
			FooterText:  normalizeMessage(words.FooterText),
			FooterLinks: fitLabels(words.FooterLinks, len(source.FooterLinks)),
			ThanksAlt:   strings.Join(strings.Fields(words.ThanksAlt), " "),
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
	if source.LogoAlt == "" {
		t.LogoAlt = ""
	}
	if source.ThanksAlt == "" {
		t.ThanksAlt = ""
	}
	if len([]rune(t.LogoAlt)) > maxStyleLogoAltLen {
		return StyleError{Part: StyleLogoAlt, Err: LimitError{Kind: LimitStyleLogoAlt, Limit: maxStyleLogoAltLen}}
	}
	if len([]rune(t.ThanksAlt)) > maxStyleThanksAltLen {
		return StyleError{Part: StyleThanksAlt, Err: LimitError{Kind: LimitStyleThanksAlt, Limit: maxStyleThanksAltLen}}
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
