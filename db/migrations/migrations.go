// Package migrations embeds the goose SQL migration files so the binary
// carries them at build time -- no separate file deployment step needed.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
