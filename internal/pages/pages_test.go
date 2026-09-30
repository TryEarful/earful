package pages_test

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/TryEarful/earful/internal/pages"
	webpages "github.com/TryEarful/earful/web/pages"
)

const written = `---
title: Where the data lives
short_title: Data
sections: cards
last_update: 2026-03-04
---

This page describes {{if .Instance}}{{.Instance}}{{else}}this instance{{end}}.

## Who is involved

| Processor | Where |
|---|---|
{{- if .Brevo}}
| Brevo | EU (France) |
{{- end}}
{{- if .Nobody}}
| Nobody | — |
{{- end}}

## Contact

A word about it.

***

{{if .Contact}}Write to [{{.Contact}}](mailto:{{.Contact}}).{{else}}No address is published.{{end}}

<script>alert(1)</script>
`

func library(t *testing.T, facts pages.Facts) *pages.Library {
	t.Helper()
	lib, err := pages.Load(fstest.MapFS{
		"data.en.md":       {Data: []byte(written)},
		"data.es.md":       {Data: []byte("---\ntitle: Dónde están los datos\n---\n\nEn español.\n")},
		"help/voice.en.md": {Data: []byte("---\ntitle: Voice\ndraft: true\n---\n\nSpeak.\n")},
	}, facts)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return lib
}

func TestADocumentStatesOnlyWhatTheInstanceSupplies(t *testing.T) {
	bare := library(t, pages.Facts{"Instance": "", "Brevo": false, "Nobody": true, "Contact": ""})
	page, _ := bare.Page("data", "en")
	for _, want := range []string{"This page describes this instance.", "<td>Nobody</td>", "No address is published."} {
		if !strings.Contains(page.HTML, want) {
			t.Errorf("missing %q in:\n%s", want, page.HTML)
		}
	}
	for _, not := range []string{"Brevo", "mailto:"} {
		if strings.Contains(page.HTML, not) || strings.Contains(page.Markdown, not) {
			t.Errorf("states %q, which the instance did not supply", not)
		}
	}

	told := library(t, pages.Facts{"Instance": "surveys.example.org", "Brevo": true, "Nobody": false, "Contact": "privacy@example.org"})
	page, _ = told.Page("data", "en")
	for _, want := range []string{"describes surveys.example.org.", "<td>Brevo</td>", `href="mailto:privacy@example.org"`} {
		if !strings.Contains(page.HTML, want) {
			t.Errorf("missing %q in:\n%s", want, page.HTML)
		}
	}
}

// What an operator configured is text. It is not Markdown, and it is
// certainly not HTML.
func TestAFactIsNotRead(t *testing.T) {
	lib := library(t, pages.Facts{
		"Instance": "*.example.org <b>bold</b> [x](http://evil.example)",
		"Brevo":    false, "Nobody": false, "Contact": "first_last_name@example.org",
	})
	page, _ := lib.Page("data", "en")
	for _, not := range []string{"<b>", "<em>", "<strong>", `href="http://evil.example"`} {
		if strings.Contains(page.HTML, not) {
			t.Errorf("a fact was read as markup (%s):\n%s", not, page.HTML)
		}
	}
	if !strings.Contains(page.HTML, "first_last_name@example.org</a>") {
		t.Errorf("the address is not shown as it was given:\n%s", page.HTML)
	}
}

func TestHTMLInADocumentIsNotPassedThrough(t *testing.T) {
	page, _ := library(t, pages.Facts{"Instance": "", "Brevo": false, "Nobody": true, "Contact": ""}).Page("data", "en")
	if strings.Contains(page.HTML, "<script") {
		t.Errorf("a script in the document reached the page:\n%s", page.HTML)
	}
}

func TestSectionsAndTablesAreDressed(t *testing.T) {
	page, _ := library(t, pages.Facts{"Instance": "", "Brevo": true, "Nobody": false, "Contact": ""}).Page("data", "en")
	if got := strings.Count(page.HTML, `<section class="card">`); got != 2 {
		t.Errorf("%d cards, want one for each of the 2 sections:\n%s", got, page.HTML)
	}
	if strings.Count(page.HTML, "<section") != strings.Count(page.HTML, "</section>") {
		t.Errorf("a section is left open:\n%s", page.HTML)
	}
	// A rule ends the last section, and is not drawn.
	last := page.HTML[strings.LastIndex(page.HTML, "</section>"):]
	if strings.Contains(page.HTML, "<hr") || !strings.Contains(last, "No address is published.") || strings.Contains(last, "A word about it.") {
		t.Errorf("what follows the rule is not outside the sections:\n%s", page.HTML)
	}
	if !strings.Contains(page.HTML, `<div class="table-scroll" tabindex="0"><table class="responses">`) {
		t.Errorf("the table is not dressed:\n%s", page.HTML)
	}
	if !strings.HasPrefix(strings.TrimSpace(page.HTML), "<p>This page describes") {
		t.Errorf("what comes before the first section is not left as it is:\n%s", page.HTML)
	}
}

func TestWhatAReaderTakesAway(t *testing.T) {
	page, _ := library(t, pages.Facts{"Instance": "surveys.example.org", "Brevo": true, "Nobody": false, "Contact": ""}).Page("data", "en")
	for _, want := range []string{
		"title: Where the data lives\n", "last_update: 2026-03-04\n",
		"# Where the data lives\n", "This page describes surveys.example.org.", "| Brevo | EU (France) |",
	} {
		if !strings.Contains(page.Markdown, want) {
			t.Errorf("missing %q in:\n%s", want, page.Markdown)
		}
	}
	if strings.Contains(page.Markdown, "{{") {
		t.Errorf("the reader is given the document's machinery:\n%s", page.Markdown)
	}
	if page.ShortTitle != "Data" || page.LastUpdate.Format(pages.DateLayout) != "2026-03-04" {
		t.Errorf("front matter read as %q, %s", page.ShortTitle, page.LastUpdate)
	}
}

func TestADocumentNotTranslatedIsServedInTheSource(t *testing.T) {
	lib := library(t, pages.Facts{"Instance": "", "Brevo": false, "Nobody": true, "Contact": ""})
	if page, _ := lib.Page("data", "es"); page.Lang != "es" || page.Title != "Dónde están los datos" {
		t.Errorf("the translation is not served: %+v", page)
	}
	if page, ok := lib.Page("help/voice", "es"); !ok || page.Lang != "en" {
		t.Errorf("an untranslated document is not served in the source: %+v", page)
	}
	if _, ok := lib.Page("nowhere", "en"); ok {
		t.Error("a document that does not exist was found")
	}
	if page, _ := lib.Page("help/voice", "en"); !page.Draft || page.ShortTitle != "Voice" {
		t.Errorf("front matter read as %+v", page)
	}
	if got := strings.Join(lib.Addresses(), " "); got != "data help/voice" {
		t.Errorf("addresses = %q", got)
	}
}

func TestLoadRefusesWhatCouldNotBeServed(t *testing.T) {
	facts := pages.Facts{"Instance": ""}
	for name, files := range map[string]fstest.MapFS{
		"no front matter":          {"a.en.md": {Data: []byte("Just words.\n")}},
		"front matter left open":   {"a.en.md": {Data: []byte("---\ntitle: A\n\nWords.\n")}},
		"no title":                 {"a.en.md": {Data: []byte("---\ndraft: true\n---\n\nWords.\n")}},
		"no language in the name":  {"a.md": {Data: []byte("---\ntitle: A\n---\n\nWords.\n")}},
		"a fact nobody supplies":   {"a.en.md": {Data: []byte("---\ntitle: A\n---\n\n{{.Regoin}}\n")}},
		"a date that is not one":   {"a.en.md": {Data: []byte("---\ntitle: A\nlast_update: yesterday\n---\n\nWords.\n")}},
		"a translation of nothing": {"a.es.md": {Data: []byte("---\ntitle: A\n---\n\nPalabras.\n")}},
	} {
		if _, err := pages.Load(files, facts); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestStampWritesTwoLinesAndNoOthers(t *testing.T) {
	const file = "---\n# kept by hand\ntitle: Terms\ndraft: true\n---\n\nThe first version.\n"
	day := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)

	out, changed, err := pages.Stamp("terms.en.md", []byte(file), day)
	if err != nil || !changed {
		t.Fatalf("stamp: changed=%v err=%v", changed, err)
	}
	hash := pages.Hash("The first version.\n")
	want := "---\n# kept by hand\ntitle: Terms\ndraft: true\nhash: " + hash + "\nlast_update: 2026-09-29\n---\n\nThe first version.\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}

	// Stamped, it is left alone, on any later day.
	again, changed, _ := pages.Stamp("terms.en.md", out, day.AddDate(0, 1, 0))
	if changed || string(again) != string(out) {
		t.Errorf("a document that had not changed was stamped again")
	}

	// A change to the front matter is not a change to the document.
	retitled := strings.Replace(string(out), "title: Terms", "title: Terms of use", 1)
	if _, changed, _ := pages.Stamp("terms.en.md", []byte(retitled), day.AddDate(0, 1, 0)); changed {
		t.Errorf("a new title moved the date of the text")
	}

	// A change to the body is, and the date is the day it was stamped.
	edited := strings.Replace(string(out), "first", "second", 1)
	out, changed, _ = pages.Stamp("terms.en.md", []byte(edited), day.AddDate(0, 1, 0))
	if !changed || !strings.Contains(string(out), "last_update: 2026-10-29\n") ||
		!strings.Contains(string(out), "hash: "+pages.Hash("The second version.\n")+"\n") ||
		strings.Count(string(out), "hash:") != 1 {
		t.Errorf("an edited document was not stamped afresh:\n%s", out)
	}
}

// The documents in the binary are the ones that matter. Their front
// matter is written by `make pages`, and this is what makes sure it
// was: an edit released with the date of the text it replaced would
// tell a reader that nothing had changed.
func TestEveryDocumentIsStamped(t *testing.T) {
	err := walk(func(name string, raw []byte) {
		doc, err := pages.Parse(name, raw)
		if err != nil {
			t.Error(err)
			return
		}
		if doc.Meta["hash"] != pages.Hash(doc.Body) {
			t.Errorf("web/pages/%s has changed since it was stamped: make pages", name)
		}
		if _, err := time.Parse(pages.DateLayout, doc.Meta["last_update"]); err != nil {
			t.Errorf("web/pages/%s has no last_update: make pages", name)
		}
		if doc.Lang != pages.Source && doc.Meta["source_hash"] == "" {
			t.Errorf("web/pages/%s does not say which text it translates: make text-accept", name)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func walk(each func(name string, raw []byte)) error {
	top, err := webpages.FS.ReadDir(".")
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range top {
		if !entry.IsDir() {
			names = append(names, entry.Name())
			continue
		}
		inside, err := webpages.FS.ReadDir(entry.Name())
		if err != nil {
			return err
		}
		for _, file := range inside {
			names = append(names, entry.Name()+"/"+file.Name())
		}
	}
	for _, name := range names {
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		raw, err := webpages.FS.ReadFile(name)
		if err != nil {
			return err
		}
		each(name, raw)
	}
	return nil
}
