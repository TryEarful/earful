package uitext_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/uitext"
)

// These tests read the source rather than run it. A message is named in
// a template and written in a file, and nothing but a reader of both
// can say that the two agree; a page that is rarely drawn would
// otherwise be the first to find out.

// unmoved lists the templates whose wording is still written in them.
// A template leaves the list when its wording has moved to web/text,
// and the list is empty when the move is done.
var unmoved = map[string]bool{
	"admin_templ.go":   true,
	"respond_templ.go": true,
	"results_templ.go": true,
	"stats_templ.go":   true,
	"surveys_templ.go": true,
}

// notWording lists what reads like wording and is not: a name that is
// the same in every language.
var notWording = map[string]bool{
	"Earful": true,
	// The shape of an invite code, which is not a word in any language.
	"earful-xxxx-xxxx-xxxx": true,
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}

// sourceFiles parses the program's own Go, generated templates included
// and tests left out.
func sourceFiles(t *testing.T, root string) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, dir := range []string{"cmd", "internal", "web"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			files[rel] = file
			return nil
		})
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
	}
	return fset, files
}

var namePattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)

// The functions that take a message's name, and where in their
// arguments it is.
var takesName = map[string]int{
	"t": 1, "tn": 1, "thtml": 1,
	"T": 0, "N": 0, "HTML": 0,
}

// use is one place a name is written.
type use struct {
	name   string
	where  string
	prefix bool
}

func literal(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

func isIDType(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == "ID"
	case *ast.SelectorExpr:
		pkg, ok := e.X.(*ast.Ident)
		return ok && pkg.Name == "uitext" && e.Sel.Name == "ID"
	case *ast.MapType:
		return isIDType(e.Value) || isIDType(e.Key)
	case *ast.ArrayType:
		return isIDType(e.Elt)
	}
	return false
}

// uses finds every name written in the source, and every place a name
// is put together rather than written.
func uses(fset *token.FileSet, files map[string]*ast.File) (found []use, assembled []string) {
	for _, file := range files {
		at := func(n ast.Node) string {
			pos := fset.Position(n.Pos())
			return pos.Filename + ":" + strconv.Itoa(pos.Line)
		}
		add := func(n ast.Expr, prefix bool) {
			if name, ok := literal(n); ok && namePattern.MatchString(name) {
				found = append(found, use{name: name, where: at(n), prefix: prefix})
			}
		}
		addAll := func(n ast.Node) {
			ast.Inspect(n, func(n ast.Node) bool {
				if expr, ok := n.(ast.Expr); ok {
					add(expr, false)
				}
				return true
			})
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CallExpr:
				var fn string
				switch f := node.Fun.(type) {
				case *ast.Ident:
					fn = f.Name
				case *ast.SelectorExpr:
					fn = f.Sel.Name
				}
				if fn == "jsText" {
					// A script is given its messages by name or by the
					// beginning of their names.
					for _, arg := range node.Args {
						add(arg, true)
					}
					return true
				}
				if isIDType(node.Fun) {
					for _, arg := range node.Args {
						add(arg, false)
					}
					return true
				}
				index, ok := takesName[fn]
				if !ok || index >= len(node.Args) {
					return true
				}
				switch arg := node.Args[index].(type) {
				case *ast.BasicLit:
					add(arg, false)
				case *ast.BinaryExpr:
					assembled = append(assembled, at(arg))
				}
			case *ast.FuncDecl:
				if node.Type.Results == nil || node.Body == nil {
					return true
				}
				for _, result := range node.Type.Results.List {
					if isIDType(result.Type) {
						addAll(node.Body)
					}
				}
			case *ast.CompositeLit:
				if node.Type != nil && isIDType(node.Type) {
					addAll(node)
				}
			case *ast.ValueSpec:
				if node.Type != nil && isIDType(node.Type) {
					for _, value := range node.Values {
						addAll(value)
					}
				}
			}
			return true
		})
	}
	return found, assembled
}

func embedded(t *testing.T) *uitext.Catalog {
	t.Helper()
	return uitext.Embedded()
}

func TestEveryNameUsedHasAMessage(t *testing.T) {
	fset, files := sourceFiles(t, repoRoot(t))
	found, assembled := uses(fset, files)
	source := embedded(t).Written(uitext.Source)

	for _, where := range assembled {
		t.Errorf("%s: a message's name is put together here; write it out, so that it can be found", where)
	}
	for _, u := range found {
		if _, ok := source[uitext.ID(u.name)]; ok {
			continue
		}
		if u.prefix && hasPrefix(source, u.name) {
			continue
		}
		t.Errorf("%s: %q has no message in active.%s.toml", u.where, u.name, uitext.Source)
	}
}

func hasPrefix[V any](messages map[uitext.ID]V, prefix string) bool {
	for id := range messages {
		if strings.HasPrefix(string(id), prefix+".") {
			return true
		}
	}
	return false
}

// A message nothing names is wording nobody reads, and a translator
// would still be asked to translate it.
func TestEveryMessageIsUsed(t *testing.T) {
	fset, files := sourceFiles(t, repoRoot(t))
	found, _ := uses(fset, files)
	used := map[string]bool{}
	var prefixes []string
	for _, u := range found {
		used[u.name] = true
		if u.prefix {
			prefixes = append(prefixes, u.name+".")
		}
	}
	var unused []string
next:
	for id := range embedded(t).Written(uitext.Source) {
		if used[string(id)] {
			continue
		}
		for _, prefix := range prefixes {
			if strings.HasPrefix(string(id), prefix) {
				continue next
			}
		}
		unused = append(unused, string(id))
	}
	sort.Strings(unused)
	for _, id := range unused {
		t.Errorf("%s is in active.%s.toml and nothing uses it", id, uitext.Source)
	}
}

var (
	placeholder = regexp.MustCompile(`\{\{\s*\.([A-Za-z0-9_]+)\s*\}\}`)
	tag         = regexp.MustCompile(`</?([A-Za-z][A-Za-z0-9]*)[^>]*>`)
	allowedTags = map[string]bool{"strong": true, "em": true, "a": true, "code": true, "small": true, "br": true}
)

func set(matches [][]string) map[string]bool {
	out := map[string]bool{}
	for _, m := range matches {
		out[m[1]] = true
	}
	return out
}

func bag(matches [][]string) string {
	var out []string
	for _, m := range matches {
		out = append(out, strings.ToLower(m[0]))
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// requiredForms is what a language needs of a message that depends on
// a number. go-i18n keeps the rules to itself, so the languages written
// in web/text are listed here; a language not listed fails, which is
// the reminder to add it.
var requiredForms = map[string][]string{
	"en": {"one", "other"},
	"es": {"one", "many", "other"},
}

func TestMarkupIsOnlyWhereItIsExpected(t *testing.T) {
	c := embedded(t)
	for _, lang := range append([]string{uitext.Source}, c.Translations()...) {
		for id, msg := range c.Written(lang) {
			for form, text := range uitext.Forms(msg) {
				tags := tag.FindAllStringSubmatch(text, -1)
				if !strings.HasSuffix(string(id), uitext.HTMLSuffix) {
					if strings.Contains(text, "<") {
						t.Errorf("%s (%s, %s) contains \"<\" and its name does not end in %s", id, lang, form, uitext.HTMLSuffix)
					}
					continue
				}
				for _, m := range tags {
					if !allowedTags[strings.ToLower(m[1])] {
						t.Errorf("%s (%s, %s): <%s> is not among the tags a message may contain", id, lang, form, m[1])
					}
					if strings.Contains(m[0], "{{") {
						t.Errorf("%s (%s, %s): a placeholder inside a tag is not escaped for where it is", id, lang, form)
					}
				}
			}
		}
	}
}

// A translation says what its source says, to the same people, about
// the same things: the same placeholders, the same markup, and a
// wording for every number its language distinguishes.
func TestTranslationsMatchTheSource(t *testing.T) {
	c := embedded(t)
	source := c.Written(uitext.Source)
	for _, lang := range c.Translations() {
		forms, ok := requiredForms[lang]
		if !ok {
			t.Errorf("%s has a file and no entry in requiredForms", lang)
			continue
		}
		written := c.Written(lang)
		for id := range source {
			if _, ok := written[id]; !ok {
				t.Errorf("%s is not in active.%s.toml", id, lang)
			}
		}
		for id, msg := range written {
			from, ok := source[id]
			if !ok {
				t.Errorf("%s is in active.%s.toml and not in the source", id, lang)
				continue
			}
			if msg.Hash == "" {
				t.Errorf("%s (%s) has no hash, so nothing says which wording it translates; make text-accept writes it", id, lang)
			}
			counted := len(uitext.Forms(from)) > 1
			got := uitext.Forms(msg)
			if counted {
				for _, form := range forms {
					if got[form] == "" {
						t.Errorf("%s (%s) depends on a number and has no %q form", id, lang, form)
					}
				}
			} else if len(got) > 1 {
				t.Errorf("%s (%s) has forms for a number its source does not depend on", id, lang)
			}
			want := set(placeholder.FindAllStringSubmatch(from.Other, -1))
			for form, text := range got {
				have := set(placeholder.FindAllStringSubmatch(text, -1))
				if form == "one" {
					// "1 invite" has no need of the number it is for.
					delete(have, "Count")
					w := map[string]bool{}
					for k := range want {
						if k != "Count" {
							w[k] = true
						}
					}
					if !sameSet(have, w) {
						t.Errorf("%s (%s, %s): placeholders %v, source has %v", id, lang, form, keys(have), keys(w))
					}
					continue
				}
				if !sameSet(have, want) {
					t.Errorf("%s (%s, %s): placeholders %v, source has %v", id, lang, form, keys(have), keys(want))
				}
				if a, b := bag(tag.FindAllStringSubmatch(text, -1)), bag(tag.FindAllStringSubmatch(from.Other, -1)); a != b {
					t.Errorf("%s (%s, %s): markup %q, source has %q", id, lang, form, a, b)
				}
			}
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// worded are the attributes a reader reads or hears.
var worded = map[string]bool{"placeholder": true, "aria-label": true, "title": true, "alt": true}

var (
	attribute = regexp.MustCompile(`([A-Za-z][A-Za-z0-9-]*)="([^"]*)"`)
	letters   = regexp.MustCompile(`\pL{2,}`)
	entity    = regexp.MustCompile(`&[a-z]+;|&#[0-9]+;`)
)

// wordingIn returns the wording written into a template's own HTML:
// text between tags, and the attributes that are read. written is the
// template's static HTML in order, with \x00 wherever a value is put in.
func wordingIn(written string) []string {
	var out []string
	note := func(text string) {
		text = strings.TrimSpace(strings.ReplaceAll(entity.ReplaceAllString(text, " "), "\x00", " "))
		if text != "" && letters.MatchString(text) && !notWording[text] {
			out = append(out, text)
		}
	}
	for len(written) > 0 {
		open := strings.IndexByte(written, '<')
		if open < 0 {
			note(written)
			break
		}
		note(written[:open])
		end := strings.IndexByte(written[open:], '>')
		if end < 0 {
			break
		}
		inside := written[open : open+end+1]
		written = written[open+end+1:]
		for _, m := range attribute.FindAllStringSubmatch(inside, -1) {
			if worded[strings.ToLower(m[1])] {
				note(m[2])
			}
		}
		// What a script or a style contains is not read by anyone.
		for _, raw := range []string{"script", "style"} {
			if strings.HasPrefix(strings.ToLower(inside), "<"+raw) {
				if end := strings.Index(strings.ToLower(written), "</"+raw); end >= 0 {
					written = written[end:]
				}
			}
		}
	}
	return out
}

// prose reports whether a string in the code reads as wording: words
// with a space between them, or a word that begins a sentence. A class
// name, a path or a field name does neither.
func prose(s string) bool {
	s = strings.TrimSpace(s)
	if notWording[s] || !letters.MatchString(s) || namePattern.MatchString(s) {
		return false
	}
	if strings.ContainsAny(s, "/=_{}<>") || strings.HasPrefix(s, "#") {
		return false
	}
	first := []rune(s)[0]
	spaced := regexp.MustCompile(`\pL[ ,.]+\pL|\pL{2,}[.!?:)]$|^[(—–-] ?\pL`).MatchString(s)
	return spaced || (first >= 'A' && first <= 'Z' && letters.MatchString(s))
}

// A template names its messages and contains none. What it does contain
// is looked for in the code templ generates, where a template's HTML is
// a run of string constants and what it computes is ordinary Go.
func TestTemplatesContainNoWording(t *testing.T) {
	root := repoRoot(t)
	fset, files := sourceFiles(t, root)
	seen := map[string]bool{}
	for path, file := range files {
		base := filepath.Base(path)
		if filepath.Dir(path) != filepath.Join("web", "templates") || !strings.HasSuffix(base, "_templ.go") {
			continue
		}
		seen[base] = true

		var html strings.Builder
		var found []string
		named := map[ast.Node]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var fn string
			switch f := call.Fun.(type) {
			case *ast.Ident:
				fn = f.Name
			case *ast.SelectorExpr:
				fn = f.Sel.Name
			}
			if fn == "WriteString" && len(call.Args) == 3 {
				if chunk, ok := literal(call.Args[2]); ok {
					html.WriteString(chunk)
					html.WriteByte(0)
					named[call.Args[2]] = true
				}
			}
			if _, ok := takesName[fn]; ok || fn == "jsText" {
				// Its arguments are names and values, not wording.
				for _, arg := range call.Args {
					named[arg] = true
				}
			}
			return true
		})
		found = append(found, wordingIn(html.String())...)
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil || named[n] {
				return false
			}
			if _, ok := n.(*ast.ImportSpec); ok {
				return false
			}
			if expr, ok := n.(ast.Expr); ok {
				if s, ok := literal(expr); ok && prose(s) {
					found = append(found, s+"  ("+strconv.Itoa(fset.Position(n.Pos()).Line)+")")
				}
			}
			return true
		})

		switch {
		case unmoved[base] && len(found) == 0:
			t.Errorf("%s contains no wording: take it off the unmoved list", base)
		case !unmoved[base]:
			for _, text := range found {
				t.Errorf("%s contains wording, which belongs in web/text: %q", base, text)
			}
		}
	}
	for base := range unmoved {
		if !seen[base] {
			t.Errorf("%s is on the unmoved list and does not exist", base)
		}
	}
}

// The reading is only worth having if it finds what is there.
func TestTheReadingFindsWording(t *testing.T) {
	got := wordingIn("<main><h1>Thank you</h1><p class=\"muted\">\x00</p><input placeholder=\"Your name\" name=\"q\">" +
		"<svg viewBox=\"0 0 24 24\"><path d=\"M22 2 11 13\"></path></svg><script>var words = \"in a script\"</script>" +
		"<a href=\"/trust\">\x00</a> — <span title=\"Shown on hover\">3</span></main>")
	want := []string{"Thank you", "Your name", "Shown on hover"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("found %q, want %q", got, want)
	}
	for s, want := range map[string]bool{
		"Thank you — ":          true,
		" (suggested)":          true,
		"Original":              true,
		"one answer":            true,
		"%d answers":            true,
		"button-link":           false,
		"q_":                    false,
		"/surveys/":             false,
		"respond.submit.label":  false,
		"yes":                   false,
		"Earful":                false,
		"web/templates/x.templ": false,
	} {
		if prose(s) != want {
			t.Errorf("prose(%q) = %v", s, !want)
		}
	}
}
