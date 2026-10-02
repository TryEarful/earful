package static

import (
	"mime"
	"regexp"
	"strings"
	"testing"
)

// Dark mode is written twice in css/app.css: once under the
// system's dark preference, where the reader has not chosen light, and
// once for a reader who chose dark. A token changed in one and not the
// other would draw the two differently, and a pair measured in one
// would not be the pair shown in the other.
func TestDarkModeIsTheSameWhicheverWayItIsChosen(t *testing.T) {
	css, err := FS.ReadFile("css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	declaration := regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+)\s*:\s*([^;]+);`)
	block := func(re string) map[string]string {
		t.Helper()
		m := regexp.MustCompile(re).FindSubmatch(css)
		if m == nil {
			t.Fatalf("css/app.css has no block matching %s", re)
		}
		tokens := map[string]string{}
		for _, d := range declaration.FindAllSubmatch(m[1], -1) {
			tokens[string(d[1])] = strings.Join(strings.Fields(string(d[2])), " ")
		}
		return tokens
	}
	system := block(`(?s)@media \(prefers-color-scheme: dark\) \{\s*:root:not\(\[data-mode="light"\]\) \{(.*?)\}`)
	chosen := block(`(?s)\n:root\[data-mode="dark"\] \{(.*?)\}`)
	if len(system) == 0 {
		t.Fatal("dark mode under the system's preference sets no tokens")
	}
	for name, value := range system {
		if chosen[name] != value {
			t.Errorf("%s is %q under the system's preference and %q when dark is chosen", name, value, chosen[name])
		}
	}
	for name := range chosen {
		if _, ok := system[name]; !ok {
			t.Errorf("%s is set when dark is chosen and not under the system's preference", name)
		}
	}

	// Form controls and scroll bars follow a chosen display mode too.
	for _, want := range []string{
		":root[data-mode=\"light\"] {\n  color-scheme: light;",
		":root[data-mode=\"dark\"] {\n  color-scheme: dark;",
	} {
		if !strings.Contains(string(css), want) {
			t.Errorf("css/app.css lacks %q", want)
		}
	}
}

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
