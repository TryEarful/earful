// Package pages holds the application's documents: the trust page, the
// terms, the help pages. Each is a Markdown file per language, written
// and kept by hand, and embedded because the runtime image carries the
// binary and nothing else.
//
// A file's place is its address: trust.en.md is served at /trust, and
// help/voice.en.md at /help/voice.
package pages

import "embed"

//go:embed *.md */*.md
var FS embed.FS
