package templates

import (
	"strings"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/uitext"
)

// styleImageAccept is what the Style tab's file fields offer to choose
// from. The server decides what a file is by its content; this only
// narrows the browser's file picker.
const styleImageAccept = "image/png,image/jpeg,image/webp"

// styleImageFieldData is one picture's field on the Style tab.
type styleImageFieldData struct {
	// Field names the file input; the box that removes the picture is
	// the same name with _remove.
	Field string
	Part  domain.StylePart
	Hint  string
	// Current is the picture the draft has, shown beside the field.
	Current domain.StyleImage
	// Plate shows the current picture on the light plate a respondent
	// sees a logo on.
	Plate bool
}

// id is the file input's id, which the error summary links to.
func (f styleImageFieldData) id() string { return "style-" + strings.ReplaceAll(f.Field, "_", "-") }

func (f styleImageFieldData) hintID() string { return f.id() + "-hint" }

// hook is the class a script or a test finds the field by.
func (f styleImageFieldData) hook() string {
	return "js-style-image-" + strings.ReplaceAll(f.Field, "_", "-")
}

// labelID is the message that names the field.
func (f styleImageFieldData) labelID() uitext.ID {
	switch f.Part {
	case domain.StyleBanner:
		return "style.header.banner_html"
	case domain.StyleThanksImage:
		return "style.thanks.image_html"
	}
	return "style.header.logo_html"
}

// removeID is the message on the box that removes the picture, which
// names the picture: a page with three such boxes says which is which.
func (f styleImageFieldData) removeID() uitext.ID {
	switch f.Part {
	case domain.StyleBanner:
		return "style.image.remove_banner"
	case domain.StyleThanksImage:
		return "style.image.remove_thanks"
	}
	return "style.image.remove_logo"
}

func (f styleImageFieldData) removeName() string { return f.Field + "_remove" }

// describedBy lists what describes the file input: its hint, and the
// problem beside it where a save was refused over this field.
func (f styleImageFieldData) describedBy(problem bool) string {
	if problem {
		return f.hintID() + " style-error-field"
	}
	return f.hintID()
}

// styleSwitch names the parts of a survey's header or footer switch:
// the field it posts, the marker beside it, and the ids that tie the
// switch to the heading that labels it and the hint that describes it.
type styleSwitch string

func (s styleSwitch) field() string     { return "custom_" + string(s) }
func (s styleSwitch) offered() string   { return string(s) + "_offered" }
func (s styleSwitch) hook() string      { return "js-custom-" + string(s) }
func (s styleSwitch) id() string        { return "style-" + string(s) + "-switch" }
func (s styleSwitch) headingID() string { return "style-" + string(s) + "-heading" }
func (s styleSwitch) hintID() string    { return "style-" + string(s) + "-hint" }
