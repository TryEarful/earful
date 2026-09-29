// Package text holds the interface's wording: every sentence, label and
// message the application shows, one file per language. The files are
// embedded because the runtime image carries the binary and nothing
// else, so a file beside it would not be there to read.
package text

import "embed"

//go:embed active.*.toml
var FS embed.FS
