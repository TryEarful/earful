package domain

import "time"

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
