package pages_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/pages"
)

// Documents follow the same rule as the interface's messages: no dash
// in anything a reader reads (docs/style-guide.md). Markdown's own
// syntax is made of hyphens, so it is removed before looking.
var (
	docDash     = regexp.MustCompile(`[-\x{2010}-\x{2015}\x{2212}]`)
	docNotProse = []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*-\s`),                                // a list item
		regexp.MustCompile(`(?m)^\|?[\s|:-]+\|?$`),                       // a table's rule
		regexp.MustCompile(`(?m)^(\*\*\*|---)$`),                         // a thematic break
		regexp.MustCompile(`\{\{[^}]*\}\}`),                              // a template action
		regexp.MustCompile("`[^`]*`"),                                    // code
		regexp.MustCompile(`\]\([^)]*\)`),                                // a link's target
		regexp.MustCompile(`\b[a-z0-9]+(?:[.-][a-z0-9]+)*\.[a-z]{2,}\b`), // an address
		regexp.MustCompile(`\b[a-z]{2,3}-[A-Z]{2}\b`),                    // a language tag
	}
)

func TestDocumentsContainNoDashes(t *testing.T) {
	err := walk(func(name string, raw []byte) {
		doc, err := pages.Parse(name, raw)
		if err != nil {
			t.Error(err)
			return
		}
		text := doc.Body + "\n" + doc.Meta["title"] + "\n" + doc.Meta["short_title"]
		for _, re := range docNotProse {
			text = re.ReplaceAllString(text, " ")
		}
		for n, line := range strings.Split(text, "\n") {
			if m := docDash.FindString(line); m != "" {
				t.Errorf("web/pages/%s: %q in %q (line %d of the text)", name, m, strings.TrimSpace(line), n+1)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}
