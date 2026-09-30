package static

import (
	"mime"
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

// The fonts are served with a type a browser accepts under nosniff.
func TestFontsHaveTheirType(t *testing.T) {
	for _, name := range []string{"fonts/manrope-latin.woff2", "fonts/manrope-latin-ext.woff2"} {
		if _, err := FS.ReadFile(name); err != nil {
			t.Errorf("%s is not embedded: %v", name, err)
		}
	}
	if got := mime.TypeByExtension(".woff2"); got != "font/woff2" {
		t.Errorf(".woff2 is served as %q, want font/woff2", got)
	}
}
