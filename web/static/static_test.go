package static

import (
	"regexp"
	"testing"
)

// A class that starts with js- is how a script or a test finds an
// element, and nothing else. Styling one would tie the look of the page
// to its behaviour again, so a restyle could break a script or a test.
// The convention is described in CONTRIBUTING.md.
func TestStylesheetsDoNotStyleHooks(t *testing.T) {
	hook := regexp.MustCompile(`\.js-[a-z0-9-]+`)
	sheets, err := FS.ReadDir("css")
	if err != nil {
		t.Fatal(err)
	}
	for _, sheet := range sheets {
		css, err := FS.ReadFile("css/" + sheet.Name())
		if err != nil {
			t.Fatal(err)
		}
		if m := hook.FindAll(css, -1); len(m) > 0 {
			t.Errorf("css/%s styles %q; style a class without the js- prefix instead", sheet.Name(), m)
		}
	}
}
