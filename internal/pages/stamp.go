package pages

import (
	"fmt"
	"strings"
	"time"
)

// Stamp brings a file's front matter up to date with its body: hash is
// the hash of the body, and last_update the day the hash last changed.
// It returns the file as it should be, and whether that differs from
// what it was given.
//
// Only those two lines are written. The file is kept by hand, and the
// rest of it is left exactly as its writer left it.
func Stamp(name string, raw []byte, today time.Time) ([]byte, bool, error) {
	doc, err := Parse(name, raw)
	if err != nil {
		return nil, false, err
	}
	hash := Hash(doc.Body)
	if doc.Meta["hash"] == hash && doc.Meta["last_update"] != "" {
		return raw, false, nil
	}
	out := Set(string(raw), "hash", hash)
	out = Set(out, "last_update", today.Format(DateLayout))
	return []byte(out), true, nil
}

// Set writes one line of a file's front matter, replacing the line that
// has the name or adding one at the end.
func Set(file, name, value string) string {
	lines := strings.Split(file, "\n")
	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closing = i
			break
		}
		key, _, ok := strings.Cut(lines[i], ":")
		if ok && strings.TrimSpace(key) == name {
			lines[i] = fmt.Sprintf("%s: %s", name, value)
			return strings.Join(lines, "\n")
		}
	}
	if closing < 0 {
		return file
	}
	added := append([]string{}, lines[:closing]...)
	added = append(added, fmt.Sprintf("%s: %s", name, value))
	added = append(added, lines[closing:]...)
	return strings.Join(added, "\n")
}
