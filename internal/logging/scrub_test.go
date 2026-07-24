package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/logging"
)

func newLogger(buf *bytes.Buffer) *slog.Logger {
	base := slog.NewJSONHandler(buf, nil)
	return slog.New(logging.NewScrubbingHandler(base))
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("decode log line: %v\nraw: %s", err, buf.String())
	}
	return m
}

func TestScrubbingHandler_RedactsSensitiveAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf)
	logger.Info("login attempt",
		"email", "person@example.com",
		"session_token", "abc123",
		"user_id", "42",
	)

	m := decode(t, &buf)
	if m["email"] != "[REDACTED]" {
		t.Errorf("email = %v, want [REDACTED]", m["email"])
	}
	if m["session_token"] != "[REDACTED]" {
		t.Errorf("session_token = %v, want [REDACTED]", m["session_token"])
	}
	if m["user_id"] != "42" {
		t.Errorf("user_id = %v, want unredacted 42 (over-redaction)", m["user_id"])
	}
}

func TestScrubbingHandler_RedactsNestedGroups(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf)
	logger.Info("answer submitted",
		slog.Group("answer", slog.String("transcript", "my secret feelings"), slog.String("question_id", "q1")),
	)

	m := decode(t, &buf)
	group, ok := m["answer"].(map[string]any)
	if !ok {
		t.Fatalf("answer group missing or wrong type: %v", m["answer"])
	}
	if group["transcript"] != "[REDACTED]" {
		t.Errorf("nested transcript = %v, want [REDACTED]", group["transcript"])
	}
	if group["question_id"] != "q1" {
		t.Errorf("nested question_id = %v, want unredacted q1", group["question_id"])
	}
}

func TestScrubbingHandler_RedactsWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	logger := newLogger(&buf).With("participant_email", "p@example.com")
	logger.Info("event")

	m := decode(t, &buf)
	if m["participant_email"] != "[REDACTED]" {
		t.Errorf("participant_email = %v, want [REDACTED]", m["participant_email"])
	}
}

func TestScrubbingHandler_DelegatesEnabled(t *testing.T) {
	base := slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError})
	h := logging.NewScrubbingHandler(base)
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("Enabled(Info) = true, want false when base handler is set to Error level")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("Enabled(Error) = false, want true")
	}
}

func TestScrubURL(t *testing.T) {
	u, err := url.Parse("/reset?email=foo@bar.com&token=abc123&ok=1")
	if err != nil {
		t.Fatal(err)
	}
	got := logging.ScrubURL(u)
	if strings.Contains(got, "foo@bar.com") {
		t.Errorf("ScrubURL result still contains the email: %s", got)
	}
	if strings.Contains(got, "abc123") {
		t.Errorf("ScrubURL result still contains the token: %s", got)
	}
	if !strings.Contains(got, "ok=1") {
		t.Errorf("ScrubURL result dropped a non-sensitive param: %s", got)
	}
}

func TestScrubURL_Nil(t *testing.T) {
	if got := logging.ScrubURL(nil); got != "" {
		t.Errorf("ScrubURL(nil) = %q, want empty", got)
	}
}
