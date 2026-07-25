package logging

import (
	"context"
	"log/slog"
	"net/url"
	"slices"
	"strings"
)

// sensitiveSubstrings is matched case-insensitively against attribute keys.
// Substring (not exact) matching is deliberate: "user_email",
// "participant_email", "session_token", "answer_text" etc. are all caught
// by construction without needing to enumerate every future key name.
var sensitiveSubstrings = []string{
	"email", "token", "transcript", "answer",
	"password", "secret", "cookie", "authorization", "credential",
}

const redacted = "[REDACTED]"

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range sensitiveSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// ScrubbingHandler wraps an slog.Handler and redacts the values of any
// attribute whose key looks sensitive (see sensitiveSubstrings), including
// attributes attached via Logger.With and attributes nested in slog.Group
// values. It never inspects log message bodies -- callers must not put
// transcripts or answer content directly into the log message.
type ScrubbingHandler struct {
	next slog.Handler
}

// NewScrubbingHandler wraps next.
func NewScrubbingHandler(next slog.Handler) *ScrubbingHandler {
	return &ScrubbingHandler{next: next}
}

func (h *ScrubbingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *ScrubbingHandler) Handle(ctx context.Context, r slog.Record) error {
	newR := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		newR.AddAttrs(scrubAttr(a))
		return true
	})
	return h.next.Handle(ctx, newR)
}

func (h *ScrubbingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	scrubbed := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		scrubbed[i] = scrubAttr(a)
	}
	return &ScrubbingHandler{next: h.next.WithAttrs(scrubbed)}
}

func (h *ScrubbingHandler) WithGroup(name string) slog.Handler {
	return &ScrubbingHandler{next: h.next.WithGroup(name)}
}

func scrubAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		scrubbed := make([]slog.Attr, len(group))
		for i, ga := range group {
			scrubbed[i] = scrubAttr(ga)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(scrubbed...)}
	}
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, redacted)
	}
	return a
}

// secretPathPrefixes are the routes that carry a credential in the *path*
// rather than in a query parameter. The segment immediately after the
// prefix is redacted and everything else is kept, so the route stays
// recognisable in logs ("/p/[REDACTED]/voice") while the secret does not
// reach the log sink.
//
// Only genuine credentials belong here. A survey share link
// (/s/{surveyID}) is public by design and stays readable -- redacting it
// would cost the ability to tell which survey a request was for and
// protect nothing.
var secretPathPrefixes = [][]string{
	// Personal invite links: routes.go says it outright -- the token is
	// the credential. Anyone holding one can answer as that participant.
	{"p"},
	// The ESP webhook secret, a configuration value shared with Brevo.
	{"webhooks", "email"},
	// Not a bearer capability (exportDownload resolves the job through
	// the session's workspace), but an export archive is the entire
	// workspace, so its identifier is not something to spray into logs.
	{"exports"},
}

// ScrubURL renders u with the values of any sensitive query parameter
// (matched via the same key vocabulary as attribute scrubbing) and any
// credential-bearing path segment replaced.
//
// Request-logging middleware must use this instead of u.String(): magic
// link tokens and participant emails travel in query strings and invite
// tokens travel in the path, neither of which attribute-key redaction
// can catch.
func ScrubURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	if scrubbed, ok := scrubPath(clone.Path); ok {
		clone.Path = scrubbed
		// Setting RawPath too keeps String() from percent-escaping the
		// brackets into %5BREDACTED%5D; url.validEncoded leaves [ and ]
		// alone, so this round-trips and stays readable.
		clone.RawPath = scrubbed
	}
	q := clone.Query()
	for key, values := range q {
		if !isSensitiveKey(key) {
			continue
		}
		for i := range values {
			values[i] = redacted
		}
		q[key] = values
	}
	clone.RawQuery = q.Encode()
	return clone.String()
}

// scrubPath replaces the credential segment of a token-bearing route,
// reporting whether it changed anything.
func scrubPath(path string) (string, bool) {
	segs := strings.Split(path, "/")
	// An absolute path yields an empty leading segment; skipping it lets
	// the prefixes below read the way the routes do.
	start := 0
	if len(segs) > 0 && segs[0] == "" {
		start = 1
	}
	for _, prefix := range secretPathPrefixes {
		secret := start + len(prefix)
		if secret >= len(segs) || segs[secret] == "" {
			continue
		}
		if !slices.Equal(segs[start:secret], prefix) {
			continue
		}
		segs[secret] = redacted
		return strings.Join(segs, "/"), true
	}
	return path, false
}
