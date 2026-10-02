package templates

import (
	"regexp"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/web/static"
)

// The browser's own chrome is painted from themeGrounds, and the page
// from --bg in the stylesheet. The two are the same colours written in
// two places, so they are read against each other here.
func TestThemeGroundsRepeatTheStylesheet(t *testing.T) {
	css, err := static.FS.ReadFile("css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	bg := func(selector string) string {
		t.Helper()
		m := regexp.MustCompile(`(?s)\n` + selector + ` \{(.*?)\}`).FindSubmatch(css)
		if m == nil {
			t.Fatalf("css/app.css has no block for %s", selector)
		}
		value := regexp.MustCompile(`(?m)^\s*--bg:\s*([^;]+);`).FindSubmatch(m[1])
		if value == nil {
			t.Fatalf("%s sets no --bg", selector)
		}
		return string(value[1])
	}
	for _, theme := range domain.Themes() {
		grounds, ok := themeGrounds[theme]
		if !ok {
			t.Errorf("theme %s has no grounds for the browser's chrome", theme)
			continue
		}
		light, dark := "", ""
		if theme == domain.ThemeEarful {
			// --bg is var(--paper) in light mode; Paper and Ink are the
			// palette's own.
			light = regexp.MustCompile(`--paper:\s*([^;]+);`).FindStringSubmatch(string(css))[1]
			dark = bg(`:root\[data-mode="dark"\]`)
		} else {
			class := themeClass(domain.Style{Theme: theme})
			if class != "theme-"+theme {
				t.Errorf("theme %s is drawn by class %q", theme, class)
			}
			light = bg(`\.` + class)
			dark = bg(`:root\[data-mode="dark"\]\.` + class + `,\n:root\[data-mode="dark"\] \.` + class)
		}
		if !strings.EqualFold(grounds.light, light) {
			t.Errorf("theme %s: light ground is %s for the chrome and %s in the stylesheet", theme, grounds.light, light)
		}
		if !strings.EqualFold(grounds.dark, dark) {
			t.Errorf("theme %s: dark ground is %s for the chrome and %s in the stylesheet", theme, grounds.dark, dark)
		}
	}
	// A theme nobody knows is drawn as the default, never as a class of
	// its own name.
	if got := themeClass(domain.Style{Theme: `x" onload="y`}); got != "" {
		t.Errorf("an unknown theme is drawn by class %q", got)
	}
}
