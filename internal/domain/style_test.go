package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewStyle(t *testing.T) {
	for _, theme := range Themes() {
		s, err := NewStyle(" " + theme + " ")
		if err != nil {
			t.Errorf("theme %q refused: %v", theme, err)
		}
		if s.ThemeName() != theme {
			t.Errorf("theme %q is named %q", theme, s.ThemeName())
		}
	}
	// The default is stored as nothing, so a draft that chose it and a
	// draft that never chose are the same draft.
	if s, _ := NewStyle(ThemeEarful); !s.IsZero() {
		t.Errorf("the default theme is stored as %+v, want the zero value", s)
	}
	if s, _ := NewStyle(""); !s.IsZero() || s.ThemeName() != ThemeEarful {
		t.Errorf("no theme is %+v, want the default", s)
	}
	for _, theme := range []string{"midnight", "Ocean", "theme-ocean", "ocean forest"} {
		if _, err := NewStyle(theme); !errors.Is(err, ErrUnknownTheme) {
			t.Errorf("theme %q: err = %v, want ErrUnknownTheme", theme, err)
		}
	}
}

func TestDraftKeepsItsStyle(t *testing.T) {
	var d Draft
	if err := d.SetStyle(Style{Theme: "midnight"}); !errors.Is(err, ErrUnknownTheme) {
		t.Fatalf("an unknown theme was set: %v", err)
	}
	if !d.Style.IsZero() {
		t.Fatalf("a refused style changed the draft: %+v", d.Style)
	}
	if err := d.SetStyle(Style{Theme: ThemeOcean}); err != nil {
		t.Fatal(err)
	}
	encoded, err := d.Encode()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseDraft(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Style.Equal(d.Style) {
		t.Errorf("style after a round trip = %+v, want %+v", back.Style, d.Style)
	}
	// A draft with no style stores none, as every draft did before a
	// survey could have one.
	plain, err := Draft{}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != `{"questions":[]}` {
		t.Errorf("an unstyled draft encodes as %s", plain)
	}
}

func TestStyleThanks(t *testing.T) {
	picture := StyleImage{SHA256: "ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12", Width: 64, Height: 40}
	for _, choice := range ThanksPictures() {
		thanks, err := StyleThanks{}.WithPicture(" " + choice + " ")
		if err != nil {
			t.Errorf("picture %q refused: %v", choice, err)
		}
		if thanks.PictureName() != choice {
			t.Errorf("picture %q is named %q", choice, thanks.PictureName())
		}
	}
	// The owl is stored as nothing, so a style that chose it is the
	// default style.
	if owl, _ := (StyleThanks{Picture: ThanksCheck}).WithPicture(ThanksOwl); !(Style{Thanks: owl}).IsZero() {
		t.Errorf("the owl is stored as %+v, want the zero value", owl)
	}
	if _, err := (StyleThanks{}).WithPicture("fireworks"); !errors.Is(err, ErrUnknownThanksPicture) {
		t.Errorf("an unknown picture: err = %v", err)
	}

	var d Draft
	if err := d.SetStyle(Style{Thanks: StyleThanks{Picture: ThanksImage}}); !errors.Is(err, ErrThanksImageMissing) {
		t.Errorf("your own picture with none: err = %v", err)
	}
	if err := d.SetStyle(Style{Thanks: StyleThanks{Picture: ThanksImage, Image: picture}}); !errors.Is(err, ErrThanksImageAlt) {
		t.Errorf("a picture with no description: err = %v", err)
	}
	own := Style{Thanks: StyleThanks{Picture: ThanksImage, Image: picture, Alt: "Our team"}}
	if err := d.SetStyle(own); err != nil {
		t.Fatal(err)
	}
	if images := d.Style.Images(); len(images) != 1 || images[0] != picture {
		t.Errorf("the style refers to %v, want the thanks picture", images)
	}
	if words := d.Style.Words(); words.ThanksAlt != "Our team" {
		t.Errorf("the picture's description is not a word to translate: %+v", words)
	}
	if got := d.Style.WithWords(StyleWords{ThanksAlt: "Ons team"}); got.Thanks.Alt != "Ons team" {
		t.Errorf("a translated description is not shown: %+v", got.Thanks)
	}

	// Another choice lets the picture go, so the draft refers only to
	// what its pages show.
	drawn := own
	drawn.Thanks.Picture = ThanksEnvelope
	if err := d.SetStyle(drawn); err != nil {
		t.Fatal(err)
	}
	if !d.Style.Thanks.Image.IsZero() || d.Style.Thanks.Alt != "" || len(d.Style.Images()) != 0 {
		t.Errorf("a drawing kept the creator's picture: %+v", d.Style.Thanks)
	}
	if !d.Style.Thanks.Drawing() || d.Style.Words().ThanksAlt != "" {
		t.Errorf("the envelope is not a drawing with nothing to translate: %+v", d.Style.Thanks)
	}
	if d.Style.Equal(own) {
		t.Errorf("two thanks pictures compare equal")
	}
}

func TestStylePartsCompareWhatAPageShows(t *testing.T) {
	logo := StyleImage{SHA256: strings.Repeat("a", 64), Width: 10, Height: 10}
	link := StyleLink{Label: "Contact", URL: "https://example.com"}

	if !SameTheme("", ThemeEarful) || SameTheme(ThemeOcean, "") {
		t.Error(`Earful's theme by name and as "" should be one theme, and only that one`)
	}
	// Alternative text left behind by a removed logo is shown to nobody.
	if !(StyleHeader{Name: "A", LogoAlt: "old"}).Equal(StyleHeader{Name: "A"}) {
		t.Error("a logo's text without a logo should not tell headers apart")
	}
	if (StyleHeader{Logo: logo, LogoAlt: "Ours"}).Equal(StyleHeader{Logo: logo, LogoAlt: "Theirs"}) {
		t.Error("two logos with different text are different headers")
	}
	if !(StyleHeader{Links: nil}).Equal(StyleHeader{Links: []StyleLink{}}) {
		t.Error("no links and an empty list of links are the same header")
	}
	if (StyleFooter{Links: []StyleLink{link}}).Equal(StyleFooter{}) {
		t.Error("a footer with a link differs from one without")
	}
	// A picture the choice would not show is not part of what the page shows.
	if !(StyleThanks{Picture: ThanksCheck, Image: logo, Alt: "x"}).Equal(StyleThanks{Picture: ThanksCheck}) {
		t.Error("a leftover picture should not tell two thanks pages apart")
	}
	if !(Style{Theme: ThemeEarful}).Equal(Style{}) {
		t.Error("a style naming Earful's theme should equal the default")
	}
}

func TestStyleWordsByPart(t *testing.T) {
	words := StyleWords{
		Tagline: "t", LogoAlt: "l", HeaderLinks: []string{"h"},
		FooterText: "f", FooterLinks: []string{"g"}, ThanksAlt: "a",
	}
	header := words.Only(StyleParts{Header: true})
	if !header.Equal(StyleWords{Tagline: "t", LogoAlt: "l", HeaderLinks: []string{"h"}}) {
		t.Errorf("the header's words are %+v", header)
	}
	if got := words.Only(StyleParts{Theme: true}); !got.IsZero() {
		t.Errorf("a theme has no words, got %+v", got)
	}
	if got := words.Only(AllStyleParts); !got.Equal(words) {
		t.Errorf("every part's words should be all the words, got %+v", got)
	}

	other := StyleWords{Tagline: "T", FooterText: "F", FooterLinks: []string{"G"}, ThanksAlt: "A"}
	mixed := words.With(other, StyleParts{Footer: true})
	want := StyleWords{Tagline: "t", LogoAlt: "l", HeaderLinks: []string{"h"}, FooterText: "F", FooterLinks: []string{"G"}, ThanksAlt: "a"}
	if !mixed.Equal(want) {
		t.Errorf("taking the footer's words gave %+v", mixed)
	}
	// The labels are copied, not shared.
	mixed.FooterLinks[0] = "changed"
	if other.FooterLinks[0] != "G" {
		t.Error("With should not share the labels it took")
	}
}
