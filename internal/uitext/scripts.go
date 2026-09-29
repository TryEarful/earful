package uitext

import (
	"sort"
	"strings"
)

// A script has wording of its own: what a button says while it works,
// what is announced when a recording ends. It cannot ask for a message
// as a template can, so the page it runs on carries the messages it
// will need, as they are written, and the script fills them in.

// ForScripts returns the messages a page's scripts are given. Each name
// is a message, or the beginning of the names of several: "js.voice" is
// every message whose name begins "js.voice.".
//
// A message with one wording is given as that wording, and one that
// depends on a number as its wordings by form, for the script to choose
// between as the language of the page chooses. A message that has not
// been translated is given in the source language.
func (l Localizer) ForScripts(names ...string) map[string]any {
	translated := l.catalog.written[l.lang]
	source := l.catalog.written[Source]

	var ids []ID
	for id := range source {
		for _, name := range names {
			if string(id) == name || strings.HasPrefix(string(id), name+".") {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	out := make(map[string]any, len(ids))
	for _, id := range ids {
		msg, ok := translated[id]
		if !ok {
			msg = source[id]
		}
		forms := Forms(msg)
		if len(forms) == 1 {
			out[string(id)] = forms["other"]
			continue
		}
		out[string(id)] = forms
	}
	return out
}
