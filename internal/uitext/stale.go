package uitext

import (
	"crypto/sha1"
	"fmt"
	"io"
	"sort"

	"github.com/nicksnyder/go-i18n/v2/i18n"
)

// Hash identifies the wording a translation was made from: a source
// message's note for translators and its text. A translation records
// the hash of its source, and a source that no longer hashes to it has
// been reworded since. The sum is the one the goi18n command computes,
// so files kept this way can still be handed to it.
func Hash(source *i18n.Message) string {
	h := sha1.New()
	_, _ = io.WriteString(h, source.Description)
	_, _ = io.WriteString(h, source.Other)
	return fmt.Sprintf("sha1-%x", h.Sum(nil))
}

// Stale lists the messages of lang that were translated from wording
// the source no longer has, in order of name. A message with no hash
// counts: nothing says what it was translated from.
func (c *Catalog) Stale(lang string) []ID {
	source := c.written[Source]
	var stale []ID
	for id, msg := range c.written[lang] {
		from, ok := source[id]
		if ok && msg.Hash != Hash(from) {
			stale = append(stale, id)
		}
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i] < stale[j] })
	return stale
}

// Translations lists the languages that have a file, other than the
// source, in order.
func (c *Catalog) Translations() []string {
	var langs []string
	for lang := range c.written {
		if lang != Source {
			langs = append(langs, lang)
		}
	}
	sort.Strings(langs)
	return langs
}
