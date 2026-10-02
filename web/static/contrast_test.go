package static

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// updateContrast rewrites the contrast table in docs/style-guide.md from
// the stylesheet: go test ./web/static -run Contrast -update-contrast
var updateContrast = flag.Bool("update-contrast", false, "rewrite the contrast table in docs/style-guide.md")

// The themes in css/app.css, the default first. The default has no
// class: it is what :root sets.
var cssThemes = []string{"earful", "slate", "ocean", "forest"}

// themeTokens are the tokens a theme may set, and must set in both of
// its blocks. Anything else is the same in every theme: Signal, the
// status colours and their tints, type, space and shape.
var themeTokens = []string{
	"--bg", "--surface", "--surface-2", "--text", "--muted", "--link",
	"--focus", "--accent", "--accent-contrast", "--border",
	"--border-strong", "--button", "--button-text", "--ring", "--tint-info",
}

var cssDeclaration = regexp.MustCompile(`(?m)^\s*(--[a-z0-9-]+)\s*:\s*([^;]+);`)

// cssBlock reads the custom properties of the first rule whose selector
// and opening brace match re.
func cssBlock(t *testing.T, css []byte, re string) map[string]string {
	t.Helper()
	m := regexp.MustCompile(re).FindSubmatch(css)
	if m == nil {
		t.Fatalf("css/app.css has no block matching %s", re)
	}
	tokens := map[string]string{}
	for _, d := range cssDeclaration.FindAllSubmatch(m[1], -1) {
		tokens[string(d[1])] = strings.Join(strings.Fields(string(d[2])), " ")
	}
	return tokens
}

func themeBlocks(t *testing.T, css []byte, theme string) (light, chosenDark, systemDark map[string]string) {
	t.Helper()
	q := regexp.QuoteMeta(theme)
	light = cssBlock(t, css, `(?s)\n\.theme-`+q+` \{(.*?)\}`)
	chosenDark = cssBlock(t, css, `(?s)\n:root\[data-mode="dark"\]\.theme-`+q+`,\n:root\[data-mode="dark"\] \.theme-`+q+` \{(.*?)\}`)
	systemDark = cssBlock(t, css, `(?s)@media \(prefers-color-scheme: dark\) \{\s*:root:not\(\[data-mode="light"\]\)\.theme-`+q+`,\s*:root:not\(\[data-mode="light"\]\) \.theme-`+q+` \{(.*?)\}`)
	return light, chosenDark, systemDark
}

// A theme is a list of tokens and nothing else, the same list in its
// light block and its dark one, and its dark block is written twice as
// dark mode is. A token set in one block and not another would be left
// over from Earful's own look in one display mode, where nobody measured
// it against the theme's grounds.
func TestThemesSetTheSameTokensInEveryBlock(t *testing.T) {
	css, err := FS.ReadFile("css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, name := range themeTokens {
		allowed[name] = true
	}
	for _, theme := range cssThemes[1:] {
		light, chosen, system := themeBlocks(t, css, theme)
		for label, block := range map[string]map[string]string{"light": light, "dark, chosen": chosen, "dark, the system's": system} {
			for name := range block {
				if !allowed[name] {
					t.Errorf("theme %s (%s) sets %s, which a theme may not set", theme, label, name)
				}
			}
			for _, name := range themeTokens {
				if _, ok := block[name]; !ok {
					t.Errorf("theme %s (%s) does not set %s", theme, label, name)
				}
			}
		}
		for name, value := range system {
			if chosen[name] != value {
				t.Errorf("theme %s: %s is %q under the system's preference and %q when dark is chosen", theme, name, value, chosen[name])
			}
		}
	}
}

// tokensFor is what a page in theme and mode ends up with: :root, then
// dark mode, then the theme's light block, then its dark one, which is
// the order the cascade applies them in.
func tokensFor(t *testing.T, css []byte, theme, mode string) map[string]string {
	t.Helper()
	tokens := cssBlock(t, css, `(?s)\n:root \{(.*?)\n\}`)
	layer := func(block map[string]string) {
		for name, value := range block {
			tokens[name] = value
		}
	}
	if mode == "dark" {
		layer(cssBlock(t, css, `(?s)\n:root\[data-mode="dark"\] \{(.*?)\}`))
	}
	if theme != "earful" {
		light, dark, _ := themeBlocks(t, css, theme)
		layer(light)
		if mode == "dark" {
			layer(dark)
		}
	}
	return tokens
}

type rgba struct{ r, g, b, a float64 }

var (
	cssHex = regexp.MustCompile(`^#([0-9a-fA-F]{6})$`)
	cssRGB = regexp.MustCompile(`^rgb\((\d+) (\d+) (\d+)(?: / (\d+)%)?\)$`)
	cssVar = regexp.MustCompile(`^var\((--[a-z0-9-]+)\)$`)
)

// colour reads a token's value: a hex colour, rgb() with or without an
// alpha, or another token.
func colour(t *testing.T, tokens map[string]string, name string) rgba {
	t.Helper()
	value, ok := tokens[name]
	if !ok {
		t.Fatalf("%s is not set", name)
	}
	for depth := 0; depth < 8; depth++ {
		if m := cssVar.FindStringSubmatch(value); m != nil {
			if value, ok = tokens[m[1]]; !ok {
				t.Fatalf("%s refers to %s, which is not set", name, m[1])
			}
			continue
		}
		if m := cssHex.FindStringSubmatch(value); m != nil {
			n, _ := strconv.ParseUint(m[1], 16, 32)
			return rgba{float64(n >> 16), float64(n >> 8 & 0xff), float64(n & 0xff), 1}
		}
		if m := cssRGB.FindStringSubmatch(value); m != nil {
			c := rgba{a: 1}
			c.r, _ = strconv.ParseFloat(m[1], 64)
			c.g, _ = strconv.ParseFloat(m[2], 64)
			c.b, _ = strconv.ParseFloat(m[3], 64)
			if m[4] != "" {
				pct, _ := strconv.ParseFloat(m[4], 64)
				c.a = pct / 100
			}
			return c
		}
		break
	}
	t.Fatalf("%s is %q, which this test cannot read as a colour", name, value)
	return rgba{}
}

// over draws c on ground, which is opaque.
func over(c, ground rgba) rgba {
	return rgba{
		r: c.r*c.a + ground.r*(1-c.a),
		g: c.g*c.a + ground.g*(1-c.a),
		b: c.b*c.a + ground.b*(1-c.a),
		a: 1,
	}
}

func luminance(c rgba) float64 {
	channel := func(v float64) float64 {
		v /= 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.r) + 0.7152*channel(c.g) + 0.0722*channel(c.b)
}

// ratio is the WCAG 2.1 contrast ratio of two opaque colours.
func ratio(a, b rgba) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// A pair is two colours that must be told apart: text and its ground at
// 4.5:1, or a mark that carries meaning and its ground at 3:1. on is the
// ground; where it is see-through, such as a tint, it is drawn on under
// first. mix, when set, is how much of fg is mixed into the ground, for
// the chosen option, whose ground is a wash of the accent.
type pair struct {
	label     string
	fg, on    string
	under     string
	mix       float64
	textOn    string
	threshold float64
}

// contrastPairs is every pair docs/style-guide.md lists. The ratios in
// its table are worked out from the stylesheet by this test.
var contrastPairs = []pair{
	{label: "Text on page", fg: "--text", on: "--bg", threshold: 4.5},
	{label: "Text on card", fg: "--text", on: "--surface", threshold: 4.5},
	{label: "Text on a field's ground", fg: "--text", on: "--surface-2", threshold: 4.5},
	{label: "Muted on page", fg: "--muted", on: "--bg", threshold: 4.5},
	{label: "Muted on card", fg: "--muted", on: "--surface", threshold: 4.5},
	{label: "Muted on a field's ground", fg: "--muted", on: "--surface-2", threshold: 4.5},
	{label: "Link on page", fg: "--link", on: "--bg", threshold: 4.5},
	{label: "Link on card", fg: "--link", on: "--surface", threshold: 4.5},
	{label: "Link on a field's ground", fg: "--link", on: "--surface-2", threshold: 4.5},
	{label: "Text on a notice, on page", fg: "--text", on: "--tint-info", under: "--bg", threshold: 4.5},
	{label: "Text on a chosen option", textOn: "--text", fg: "--accent", on: "--surface", mix: 0.07, threshold: 4.5},
	{label: "Filled button's text on it", fg: "--button-text", on: "--button", threshold: 4.5},
	{label: "Text on the accent", fg: "--accent-contrast", on: "--accent", threshold: 4.5},
	{label: "Text on a delete button", fg: "--accent-contrast", on: "--danger", threshold: 4.5},
	{label: "Danger on page", fg: "--danger", on: "--bg", threshold: 4.5},
	{label: "Danger on card", fg: "--danger", on: "--surface", threshold: 4.5},
	{label: "Good on its tint, on card", fg: "--good", on: "--tint-good", under: "--surface", threshold: 4.5},
	{label: "Danger on its tint, on card", fg: "--danger", on: "--tint-serious", under: "--surface", threshold: 4.5},
	{label: "Warning on its tint, on card", fg: "--warning", on: "--tint-warning", under: "--surface", threshold: 4.5},
	{label: "Ink on Signal", fg: "--ink", on: "--signal", threshold: 4.5},
	{label: "Ink on Signal's soft tint", fg: "--ink", on: "--signal-soft", threshold: 4.5},
	{label: "Focus ring on page (3:1)", fg: "--focus", on: "--bg", threshold: 3},
	{label: "Focus ring on card (3:1)", fg: "--focus", on: "--surface", threshold: 3},
	{label: "Accent on card (3:1)", fg: "--accent", on: "--surface", threshold: 3},
	{label: "Filled button on page (3:1)", fg: "--button", on: "--bg", threshold: 3},
	{label: "Signal on card (3:1)", fg: "--signal", on: "--surface", threshold: 3},
	{label: "Signal on page", fg: "--signal", on: "--bg", threshold: 3},
}

func measure(t *testing.T, tokens map[string]string, p pair) float64 {
	t.Helper()
	ground := colour(t, tokens, p.on)
	if p.under != "" {
		ground = over(ground, colour(t, tokens, p.under))
	}
	fg := colour(t, tokens, p.fg)
	if p.mix > 0 {
		fg.a = p.mix
		ground = over(fg, ground)
		fg = colour(t, tokens, p.textOn)
	}
	return ratio(over(fg, ground), ground)
}

// Every pair holds its ratio in every theme and display mode. Where
// Earful's own look is below the usual ratio for a pair, which is so for
// Signal on the light page, a theme may not be below Earful: a theme
// never makes a pair harder to read than the default does.
func TestEveryPairHoldsItsContrastInEveryTheme(t *testing.T) {
	css, err := FS.ReadFile("css/app.css")
	if err != nil {
		t.Fatal(err)
	}
	modes := []string{"light", "dark"}
	measured := map[string]float64{}
	key := func(p pair, theme, mode string) string { return p.label + "|" + theme + "|" + mode }
	for _, theme := range cssThemes {
		for _, mode := range modes {
			tokens := tokensFor(t, css, theme, mode)
			for _, p := range contrastPairs {
				measured[key(p, theme, mode)] = measure(t, tokens, p)
			}
		}
	}
	const rounding = 0.005
	for _, p := range contrastPairs {
		for _, mode := range modes {
			floor := math.Min(p.threshold, measured[key(p, "earful", mode)])
			for _, theme := range cssThemes[1:] {
				if got := measured[key(p, theme, mode)]; got < floor-rounding {
					t.Errorf("%s, %s %s: %.2f, below %.2f", p.label, theme, mode, got, floor)
				}
			}
		}
	}

	var table strings.Builder
	table.WriteString("| Pair |")
	for _, theme := range cssThemes {
		for _, mode := range modes {
			fmt.Fprintf(&table, " %s %s |", strings.ToUpper(theme[:1])+theme[1:], mode)
		}
	}
	table.WriteString("\n|---|" + strings.Repeat("---|", len(cssThemes)*len(modes)) + "\n")
	for _, p := range contrastPairs {
		fmt.Fprintf(&table, "| %s |", p.label)
		for _, theme := range cssThemes {
			for _, mode := range modes {
				fmt.Fprintf(&table, " %.2f |", measured[key(p, theme, mode)])
			}
		}
		table.WriteString("\n")
	}

	// The style guide's table is these numbers. A token changed without
	// the table is a guide that describes a page nobody sees.
	guide := guidePath(t)
	raw, err := os.ReadFile(guide)
	if err != nil {
		t.Fatal(err)
	}
	const begin, end = "<!-- contrast table: written by web/static/contrast_test.go -->\n", "<!-- end of the contrast table -->"
	text := string(raw)
	from := strings.Index(text, begin)
	to := strings.Index(text, end)
	if from < 0 || to < from {
		t.Fatalf("%s has no contrast table between its markers", guide)
	}
	from += len(begin)
	if text[from:to] == table.String() {
		return
	}
	if *updateContrast {
		if err := os.WriteFile(guide, []byte(text[:from]+table.String()+text[to:]), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Errorf("the contrast table in docs/style-guide.md is not what the stylesheet gives; rewrite it with:\n\tgo test ./web/static -run Contrast -update-contrast\nwant:\n%s", table.String())
}

// The tokens a theme may set are the ones the stylesheet's comment and
// the style guide say it sets; the list is here once and they are read
// against it.
func TestTheGuideListsWhatAThemeSets(t *testing.T) {
	raw, err := os.ReadFile(guidePath(t))
	if err != nil {
		t.Fatal(err)
	}
	names := append([]string(nil), themeTokens...)
	sort.Strings(names)
	for _, name := range names {
		if !strings.Contains(string(raw), "`"+name+"`") {
			t.Errorf("docs/style-guide.md does not name %s among the tokens a theme sets", name)
		}
	}
}

func guidePath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "docs", "style-guide.md")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}

// A respondent's page is the only page drawn in a theme, and Forest's
// own green is the hue of --good, so a success or a warning there could
// not be told from the theme (docs/style-guide.md, "Themes"). The rules
// that use those colours are found in the stylesheet, and none of their
// classes may appear in the respondent's templates.
func TestAThemedPageUsesNoGoodOrWarningColour(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("css", "app.css"))
	if err != nil {
		t.Fatal(err)
	}
	rule := regexp.MustCompile(`(?s)\n([^\n{}@/][^{}]*?)\s*\{([^{}]*)\}`)
	uses := regexp.MustCompile(`var\(--(good|warning|tint-good|tint-warning)\)`)
	class := regexp.MustCompile(`\.([a-z][a-z0-9-]*)`)
	classes := map[string]bool{}
	for _, m := range rule.FindAllSubmatch(css, -1) {
		if !uses.Match(m[2]) {
			continue
		}
		found := class.FindAllSubmatch(m[1], -1)
		if len(found) == 0 {
			t.Errorf("the rule %q uses a good or warning colour with no class to keep off a themed page", strings.TrimSpace(string(m[1])))
		}
		for _, c := range found {
			classes[string(c[1])] = true
		}
	}
	if len(classes) == 0 {
		t.Fatal("found no rule using a good or warning colour; the test no longer reads the stylesheet as it is written")
	}
	templates, err := filepath.Glob(filepath.Join("..", "templates", "respond*.templ"))
	if err != nil || len(templates) == 0 {
		t.Fatalf("no respondent templates found: %v", err)
	}
	for _, path := range templates {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for name := range classes {
			if regexp.MustCompile(`[^a-z0-9-]` + regexp.QuoteMeta(name) + `[^a-z0-9-]`).Match(raw) {
				t.Errorf("%s uses .%s, which is drawn in a good or warning colour; a themed page has neither", filepath.Base(path), name)
			}
		}
	}
}
