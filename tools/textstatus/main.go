// Command textstatus lists the translations that were made from wording
// the source no longer has: the messages in web/text, and the documents
// in web/pages.
//
// A translation records the hash of the English it was made from. When
// the English is reworded the hash stops matching, and the translation
// is listed here until someone has read it against the new wording,
// changed it if it needs changing, and accepted it:
//
//	make text-status
//	make text-accept ID="respond.submit.label help/voice"
//	make text-accept            # everything listed
//
// A message is named as it is in its file, and a document by its
// address.
//
// It reports and never fails. A translation made from older wording is
// still a translation, and usually still a right one; refusing to build
// would make rewording a sentence cost a translation every time.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TryEarful/earful/internal/pages"
	"github.com/TryEarful/earful/internal/uitext"
)

func main() {
	dir := flag.String("dir", "web/text", "where the message files are")
	docs := flag.String("pages", "web/pages", "where the documents are")
	accept := flag.Bool("accept", false, "record the listed translations as made from the current wording; names as arguments, or none for all")
	flag.Parse()

	if err := run(*dir, *docs, *accept, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "textstatus:", err)
		os.Exit(1)
	}
}

func run(dir, docs string, accept bool, names []string) error {
	catalog, err := uitext.Load(os.DirFS(dir), uitext.Options{Languages: []string{uitext.Source}})
	if err != nil {
		return err
	}
	documents, err := read(docs)
	if err != nil {
		return err
	}
	source := catalog.Written(uitext.Source)
	wanted := map[uitext.ID]bool{}
	wantedDocs := map[string]bool{}
	for _, name := range names {
		if _, ok := source[uitext.ID(name)]; ok {
			wanted[uitext.ID(name)] = true
		} else if _, ok := documents[name]; ok {
			wantedDocs[name] = true
		} else {
			return fmt.Errorf("%s is neither a message nor a document", name)
		}
	}
	all := len(names) == 0

	total := 0
	for _, lang := range catalog.Translations() {
		stale := catalog.Stale(lang)
		if accept {
			var chosen []uitext.ID
			for _, id := range stale {
				if all || wanted[id] {
					chosen = append(chosen, id)
				}
			}
			path := filepath.Join(dir, "active."+lang+".toml")
			hashes := make(map[uitext.ID]string, len(chosen))
			for _, id := range chosen {
				hashes[id] = uitext.Hash(source[id])
			}
			if err := stamp(path, hashes); err != nil {
				return err
			}
			fmt.Printf("%s: %d accepted\n", lang, len(chosen))
			continue
		}
		total += len(stale)
		if len(stale) == 0 {
			fmt.Printf("%s: every translation was made from the current wording\n", lang)
			continue
		}
		fmt.Printf("%s: %d made from wording that has changed\n", lang, len(stale))
		written := catalog.Written(lang)
		for _, id := range stale {
			fmt.Printf("\n  %s\n    %s: %s\n    %s: %s\n", id, uitext.Source, source[id].Other, lang, written[id].Other)
		}
	}

	stale := staleDocuments(documents)
	if accept {
		accepted := 0
		for _, doc := range stale {
			if !all && !wantedDocs[doc.Path] {
				continue
			}
			path := filepath.Join(docs, filepath.FromSlash(doc.Path)+"."+doc.Lang+".md")
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			out := pages.Set(string(raw), "source_hash", pages.Hash(documents[doc.Path][pages.Source].Body))
			if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
				return err
			}
			accepted++
		}
		fmt.Printf("documents: %d accepted\n", accepted)
		return nil
	}
	total += len(stale)
	if len(stale) == 0 {
		fmt.Println("documents: every translation was made from the current text")
	} else {
		fmt.Printf("documents: %d translated from text that has changed\n\n", len(stale))
		for _, doc := range stale {
			fmt.Printf("  %s (%s)\n", doc.Path, doc.Lang)
		}
	}
	if total > 0 {
		fmt.Println("\nRead each against its source, change it where it needs changing, then: make text-accept")
	}
	return nil
}

// read parses every document under dir, by address and then language.
func read(dir string) (map[string]map[string]pages.Written, error) {
	documents := map[string]map[string]pages.Written{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, _ := filepath.Rel(dir, path)
		doc, err := pages.Parse(filepath.ToSlash(name), raw)
		if err != nil {
			return err
		}
		if documents[doc.Path] == nil {
			documents[doc.Path] = map[string]pages.Written{}
		}
		documents[doc.Path][doc.Lang] = doc
		return nil
	})
	return documents, err
}

// staleDocuments lists the translations made from a body the source no
// longer has, in order of address.
func staleDocuments(documents map[string]map[string]pages.Written) []pages.Written {
	var stale []pages.Written
	for _, langs := range documents {
		source, ok := langs[pages.Source]
		if !ok {
			continue
		}
		for lang, doc := range langs {
			if lang != pages.Source && doc.Meta["source_hash"] != pages.Hash(source.Body) {
				stale = append(stale, doc)
			}
		}
	}
	sort.Slice(stale, func(i, j int) bool {
		if stale[i].Path != stale[j].Path {
			return stale[i].Path < stale[j].Path
		}
		return stale[i].Lang < stale[j].Lang
	})
	return stale
}

// stamp writes each message's hash into the file at path, changing the
// one line and nothing else. The file is kept by hand, with comments
// and an order that mean something to whoever keeps it, and writing it
// out again from what was parsed would lose both.
func stamp(path string, hashes map[uitext.ID]string) error {
	if len(hashes) == 0 {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	done := map[uitext.ID]bool{}

	var out []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		out = append(out, line)
		header := strings.TrimSpace(line)
		if !strings.HasPrefix(header, "[") || !strings.HasSuffix(header, "]") {
			continue
		}
		id := uitext.ID(strings.Trim(header, "[]"))
		hash, ok := hashes[id]
		if !ok {
			continue
		}
		done[id] = true
		replaced := false
		for j := i + 1; j < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[j]), "["); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "hash") {
				lines[j] = fmt.Sprintf("hash = %q", hash)
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, fmt.Sprintf("hash = %q", hash))
		}
	}
	for id := range hashes {
		if !done[id] {
			return fmt.Errorf("%s: no [%s] in the file; a translation is written in full, under its whole name, so that its hash has a place", path, id)
		}
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}
