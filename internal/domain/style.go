package domain

import (
	"errors"
	"strings"
)

// Style is how a survey looks to the people answering it: for now, the
// theme its pages are drawn in (ADR-0018). It is part of the draft and is
// frozen into each published version with the questions, because the look
// of the page is part of what a respondent was shown (ADR-0001). The
// zero value means "not set": the survey is drawn as Earful draws every
// page, exactly as it was before a creator could choose.
//
// It is a struct of its own, and not a field or two on Draft, so that
// the rest of a survey's style (ADR-0018: a header, a footer, a picture
// for the thanks page) joins the theme here and is frozen by the same
// column.
type Style struct {
	// Theme names one of the themes in web/static/css/app.css. It is
	// empty for Earful's own, so a draft that never chose one and a
	// draft that chose the default are the same draft.
	Theme string `json:"theme,omitempty"`
}

// The themes a survey can be drawn in. ThemeEarful is the default and is
// stored as the empty string.
const (
	ThemeEarful = "earful"
	ThemeSlate  = "slate"
	ThemeOcean  = "ocean"
	ThemeForest = "forest"
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

// NewStyle builds a Style from what a creator chose, and checks it. The
// default theme is stored as the empty string.
func NewStyle(theme string) (Style, error) {
	theme = strings.TrimSpace(theme)
	if theme == ThemeEarful {
		theme = ""
	}
	s := Style{Theme: theme}
	if err := s.Validate(); err != nil {
		return Style{}, err
	}
	return s, nil
}

// IsZero reports whether nothing is set, so the survey is drawn in the
// default style.
func (s Style) IsZero() bool { return s == Style{} }

// Equal reports whether two styles would draw the same pages.
func (s Style) Equal(other Style) bool { return s == other }

// ThemeName is the theme the survey is drawn in, by name: the stored
// one, or the default where none was chosen.
func (s Style) ThemeName() string {
	if s.Theme == "" {
		return ThemeEarful
	}
	return s.Theme
}

// Validate checks that the theme is one the stylesheet has.
func (s Style) Validate() error {
	switch s.Theme {
	case "", ThemeSlate, ThemeOcean, ThemeForest:
		return nil
	}
	return ErrUnknownTheme
}

// SetStyle replaces the draft's style.
func (d *Draft) SetStyle(s Style) error {
	if err := s.Validate(); err != nil {
		return err
	}
	d.Style = s
	return nil
}
