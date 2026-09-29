// Command pages brings the front matter of the documents in web/pages
// up to date with what they say.
//
// A document's front matter carries the hash of its body and the day
// that hash last changed. Neither is written by hand: edit the document,
// then
//
//	make pages
//
// and the documents that changed are stamped with their new hash and
// today's date. A test fails the build on a document whose hash is not
// the hash of its body, so an edit cannot be released without its date.
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/TryEarful/earful/internal/pages"
)

func main() {
	dir := flag.String("dir", "web/pages", "where the documents are")
	flag.Parse()

	if err := run(*dir, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "pages:", err)
		os.Exit(1)
	}
}

func run(dir string, today time.Time) error {
	stamped := 0
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
		out, changed, err := pages.Stamp(filepath.ToSlash(name), raw, today)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		stamped++
		fmt.Println("stamped", path)
		return os.WriteFile(path, out, 0o644)
	})
	if err != nil {
		return err
	}
	if stamped == 0 {
		fmt.Println("every document is up to date")
	}
	return nil
}
