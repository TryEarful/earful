// Package logging provides structured (slog JSON) logging with built-in
// scrubbing so that emails, tokens, transcripts, and answer content never
// reach stdout. Every subcommand builds its logger through New.
package logging

import (
	"io"
	"log/slog"
)

// New builds a JSON slog.Logger writing to w, filtered through a
// ScrubbingHandler so sensitive attribute values are redacted regardless
// of which package logged them.
//
// The top-level keys are renamed to Cloud Logging's structured-logging
// special fields (severity/message) so that on Cloud Run each entry gets
// the right severity instead of DEFAULT, and error-rate alerting can
// filter on severity>=ERROR. Local tools read the same keys fine.
func New(level slog.Level, w io.Writer) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: cloudLoggingKeys,
	})
	return slog.New(NewScrubbingHandler(base))
}

// cloudLoggingKeys maps slog's default top-level keys to the field names
// Cloud Logging promotes into LogEntry (severity, message). slog spells
// warnings "WARN"; Cloud Logging wants "WARNING" — every other level name
// already matches.
func cloudLoggingKeys(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.LevelKey:
		a.Key = "severity"
		if lv, ok := a.Value.Any().(slog.Level); ok && lv == slog.LevelWarn {
			a.Value = slog.StringValue("WARNING")
		}
	case slog.MessageKey:
		a.Key = "message"
	}
	return a
}
