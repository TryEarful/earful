package uitext_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/uitext"
)

// Earful's wording joins its clauses with full stops, commas, colons and
// the middle dot, never with a dash, and writes compounds as separate
// words ("sign in", "open source"). The style guide (docs/style-guide.md)
// gives the reasons; these tests keep a dash from being written back in.

// dash is every character that reads as one: the hyphen, the Unicode
// hyphens, the figure, en and em dashes, the horizontal bar and the
// minus sign.
var dash = regexp.MustCompile(`[-\x{2010}-\x{2015}\x{2212}]`)

// notProse is what a message may contain that is not read as wording, and
// so may keep the hyphens it is spelled with: placeholders, code, markup,
// addresses, and language tags such as pt-BR.
var notProse = []*regexp.Regexp{
	regexp.MustCompile(`\{\{[^}]*\}\}`),
	regexp.MustCompile(`(?s)<code>.*?</code>`),
	regexp.MustCompile(`<[^>]*>`),
	regexp.MustCompile(`https?://\S+`),
	regexp.MustCompile(`\b[a-z0-9]+(?:[.-][a-z0-9]+)*\.[a-z]{2,}\b`),
	regexp.MustCompile(`\b[a-z]{2,3}-[A-Z]{2}\b`),
}

// readText strips from s what is not read as wording.
func Prose(s string) string {
	for _, re := range notProse {
		s = re.ReplaceAllString(s, " ")
	}
	return s
}

func TestMessagesContainNoDashes(t *testing.T) {
	c := embedded(t)
	for _, lang := range append([]string{uitext.Source}, c.Translations()...) {
		for id, msg := range c.Written(lang) {
			for form, text := range uitext.Forms(msg) {
				if m := dash.FindString(Prose(text)); m != "" {
					t.Errorf("%s (%s, %s) contains %q: %s", id, lang, form, m, text)
				}
			}
		}
	}
}

// A template's text is all in messages, save for punctuation written
// between two of them. That punctuation may not be a dash either.
func TestTemplatesContainNoDashes(t *testing.T) {
	root := repoRoot(t)
	paths, err := filepath.Glob(filepath.Join(root, "web", "templates", "*.templ"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no templates found: %v", err)
	}
	// Between two values a hyphen is spaced; within a class name or an
	// attribute it is not, and is not read.
	spaced := regexp.MustCompile(`[\x{2010}-\x{2015}\x{2212}]|\s-\s`)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if m := spaced.FindString(line); m != "" && !strings.Contains(line, "{{") && !goExpression(line) {
				t.Errorf("%s:%d contains %q: %s", filepath.Base(path), n+1, m, strings.TrimSpace(line))
			}
		}
	}
}

// goExpression reports whether a line is Go arithmetic, where a spaced
// hyphen is a minus and is never shown.
func goExpression(line string) bool {
	return regexp.MustCompile(`[\w)\]] - [\w(]`).MatchString(line) && !strings.Contains(line, "<")
}

func TestProseKeepsWhatIsRead(t *testing.T) {
	for in, want := range map[string]bool{
		"Sign in · Earful": false,
		"Sign-in":          true,
		"like <code>earful beta-codes add</code>": false,
		"use a code like pt-BR":                   false,
		"see db-ip.com":                           false,
		"now — later":                             true,
	} {
		if got := dash.MatchString(Prose(in)); got != want {
			t.Errorf("%q: dash found %v, want %v", in, got, want)
		}
	}
}
