// Package pages turns the documents in web/pages into pages the
// application can serve. A document is long-form writing (the trust
// page, the terms, a help page) and is kept as Markdown so that it can be
// written and read as a document; the short wording of the interface
// itself is in web/text and served by uitext.
//
// A document states facts about the instance serving it: where it is
// hosted, which companies it involves, whom to write to. Those differ
// from one instance to the next, so the document names them and the
// instance supplies them, and a claim the instance cannot make is left
// out rather than filled in.
package pages

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// Source is the language documents are written in. A document with no
// translation is served in this one.
const Source = "en"

// DateLayout is how last_update is written in a document.
const DateLayout = "2006-01-02"

// Facts are what a document may say about the instance, by name. A
// document that names a fact the instance does not supply fails to load,
// which is how a misspelt name is found.
type Facts map[string]any

// Written is a document as it is in its file.
type Written struct {
	// Path is the document's address without its slash, and Lang the
	// language of this file: help/voice.es.md is "help/voice" in "es".
	Path string
	Lang string
	// Meta is the front matter, and Body everything below it.
	Meta map[string]string
	Body string
}

// Page is a document ready to serve.
type Page struct {
	Path string
	Lang string
	// Title heads the page. ShortTitle names it in the window's title,
	// and is Title where the document gives none.
	Title      string
	ShortTitle string
	// Hash identifies the body as written, and LastUpdate is the day it
	// last changed. SourceHash, on a translation, is the hash of the
	// source it was made from.
	Hash       string
	LastUpdate time.Time
	SourceHash string
	// Cards draws each section in a card of its own.
	Cards bool
	// Draft marks a document that is not ready to be read. It is loaded
	// and checked like any other, and served only in development.
	Draft bool
	// HTML is the body rendered. Markdown is the document as a reader
	// would take it away: what it is, when it changed, and the body with
	// this instance's facts in it.
	HTML     string
	Markdown string
}

// Hash identifies a body. It covers the body and not the front matter
// above it, because the hash is written in the front matter, and a file
// cannot contain the hash of itself.
func Hash(body string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(body)))
	return fmt.Sprintf("sha256-%x", sum)
}

// Parse reads one file. name is its path within web/pages.
func Parse(name string, raw []byte) (Written, error) {
	base := strings.TrimSuffix(name, ".md")
	dot := strings.LastIndex(base, ".")
	if !strings.HasSuffix(name, ".md") || dot < 0 || dot == len(base)-1 || strings.Contains(base[dot:], "/") {
		return Written{}, fmt.Errorf("%s: a document is named <address>.<language>.md", name)
	}
	doc := Written{Path: base[:dot], Lang: base[dot+1:], Meta: map[string]string{}}

	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return Written{}, fmt.Errorf("%s: no front matter: the file begins with a line of ---", name)
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return Written{}, fmt.Errorf("%s: the front matter is not closed by a line of ---", name)
	}
	for _, line := range strings.Split(text[4:4+end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Written{}, fmt.Errorf("%s: front matter line %q is not \"name: value\"", name, line)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		doc.Meta[strings.TrimSpace(key)] = value
	}
	doc.Body = strings.TrimLeft(text[4+end+5:], "\n")
	return doc, nil
}

// Library is every document in every language it is written in.
type Library struct {
	pages map[string]map[string]*Page
}

var markdown = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	// HTML written in a document is not passed through: a document is
	// prose, and what it cannot say in Markdown it does not need.
)

// Load reads every document in fsys and renders it with facts.
func Load(fsys fs.FS, facts Facts) (*Library, error) {
	filled := make(map[string]any, len(facts))
	for name, value := range facts {
		if s, ok := value.(string); ok {
			value = escape(s)
		}
		filled[name] = value
	}

	lib := &Library{pages: map[string]map[string]*Page{}}
	err := fs.WalkDir(fsys, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(name) != ".md" {
			return nil
		}
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		doc, err := Parse(name, raw)
		if err != nil {
			return err
		}
		page, err := render(name, doc, filled)
		if err != nil {
			return err
		}
		if lib.pages[page.Path] == nil {
			lib.pages[page.Path] = map[string]*Page{}
		}
		lib.pages[page.Path][page.Lang] = page
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("pages: %w", err)
	}
	for address, langs := range lib.pages {
		if langs[Source] == nil {
			return nil, fmt.Errorf("pages: %s is translated and has no %s.%s.md to be a translation of", address, address, Source)
		}
	}
	return lib, nil
}

func render(name string, doc Written, facts map[string]any) (*Page, error) {
	page := &Page{
		Path:       doc.Path,
		Lang:       doc.Lang,
		Title:      doc.Meta["title"],
		ShortTitle: doc.Meta["short_title"],
		Hash:       doc.Meta["hash"],
		SourceHash: doc.Meta["source_hash"],
		Cards:      doc.Meta["sections"] == "cards",
		Draft:      doc.Meta["draft"] == "true",
	}
	if page.Title == "" {
		return nil, fmt.Errorf("%s: no title in the front matter", name)
	}
	if page.ShortTitle == "" {
		page.ShortTitle = page.Title
	}
	if written := doc.Meta["last_update"]; written != "" {
		day, err := time.Parse(DateLayout, written)
		if err != nil {
			return nil, fmt.Errorf("%s: last_update %q is not a date written as %s", name, written, DateLayout)
		}
		page.LastUpdate = day
	}

	tmpl, err := template.New(name).Option("missingkey=error").Parse(doc.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	var body bytes.Buffer
	if err := tmpl.Execute(&body, facts); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	var out bytes.Buffer
	if err := markdown.Convert(body.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	page.HTML = dress(out.String(), page.Cards)

	var taken strings.Builder
	fmt.Fprintf(&taken, "---\ntitle: %s\n", page.Title)
	if !page.LastUpdate.IsZero() {
		fmt.Fprintf(&taken, "last_update: %s\n", page.LastUpdate.Format(DateLayout))
	}
	if page.Hash != "" {
		fmt.Fprintf(&taken, "hash: %s\n", page.Hash)
	}
	fmt.Fprintf(&taken, "---\n\n# %s\n\n%s\n", page.Title, strings.TrimSpace(body.String()))
	page.Markdown = taken.String()
	return page, nil
}

// dress fits the rendered body to the application's stylesheet. A table
// scrolls sideways within the page on a narrow screen and does not push
// the page wider, and can be reached from the keyboard, since a region
// that scrolls and cannot be focused cannot be scrolled without a
// pointer; and where the document asks for cards, each section
// is one, from its heading to the next heading or to a rule. A rule
// (*** on a line of its own) is how a document says that what follows
// belongs to no section, and in a document of cards it is not drawn.
func dress(html string, cards bool) string {
	html = strings.ReplaceAll(html, "<table>", `<div class="table-scroll" tabindex="0"><table class="responses">`)
	html = strings.ReplaceAll(html, "</table>", "</table></div>")
	if !cards {
		return html
	}
	parts := strings.Split(html, "<h2")
	var out strings.Builder
	out.WriteString(parts[0])
	for _, section := range parts[1:] {
		section, after, _ := strings.Cut(section, "<hr>")
		out.WriteString(`<section class="card"><h2`)
		out.WriteString(strings.TrimRight(section, "\n"))
		out.WriteString("</section>\n")
		out.WriteString(strings.TrimLeft(after, "\n"))
	}
	return out.String()
}

// escape keeps a fact a fact. What an operator configured (a host, an
// address, the name of a place) is put into the document as text, and
// must not be read as Markdown: a name with an asterisk in it is not a
// request for emphasis.
func escape(s string) string {
	var out strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_{}[]()<>#|!", r) {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// Page returns the document at address in lang, or in the source
// language where it has not been translated.
func (l *Library) Page(address, lang string) (*Page, bool) {
	langs, ok := l.pages[address]
	if !ok {
		return nil, false
	}
	if page, ok := langs[lang]; ok {
		return page, true
	}
	return langs[Source], true
}

// Addresses lists the documents, in order.
func (l *Library) Addresses() []string {
	out := make([]string, 0, len(l.pages))
	for address := range l.pages {
		out = append(out, address)
	}
	sort.Strings(out)
	return out
}

// Languages lists the languages a document is written in, the source
// first.
func (l *Library) Languages(address string) []string {
	var out []string
	for lang := range l.pages[address] {
		if lang != Source {
			out = append(out, lang)
		}
	}
	sort.Strings(out)
	return append([]string{Source}, out...)
}
