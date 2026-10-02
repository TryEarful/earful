package domain

import (
	"errors"
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
