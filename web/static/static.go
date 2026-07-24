// Package static embeds hand-written CSS/JS assets into the binary so the
// container image needs no separate asset-copy step.
package static

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
)

//go:embed css js
var FS embed.FS

// Version is a content fingerprint of the embedded assets, computed once
// at startup. Templates append it to asset URLs (?v=...), so browsers can
// cache aggressively and still pick up changed CSS/JS on the next page
// load after a deploy — without this, a deploy's script changes hide
// behind stale caches for the whole max-age (which is exactly how the
// ALTCHA solver failed to appear during M4 verification).
var Version = computeVersion()

func computeVersion() string {
	h := sha256.New()
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := FS.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write([]byte(path))
		h.Write(content)
		return nil
	})
	if err != nil {
		// The embedded FS cannot fail to read itself; if it somehow does,
		// a per-process random-ish fallback would mask the bug — crash
		// loudly at startup instead.
		panic("static: fingerprint embedded assets: " + err.Error())
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// AssetURL returns the fingerprinted URL for an embedded asset path,
// e.g. AssetURL("css/app.css") → "/static/css/app.css?v=abc123def456".
func AssetURL(path string) string {
	return "/static/" + path + "?v=" + Version
}
