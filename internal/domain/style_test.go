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
