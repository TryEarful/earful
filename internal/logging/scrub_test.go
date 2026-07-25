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

// Credentials also travel in the path, which query scrubbing alone
// cannot reach: RequestLogger writes r.URL for every request, so an
// invite token or the ESP webhook secret would otherwise land in the log
// sink verbatim on every hit.
func TestScrubURL_RedactsCredentialPathSegments(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"invite link", "/p/inv_abc123", "/p/[REDACTED]"},
		{"invite voice socket", "/p/inv_abc123/voice", "/p/[REDACTED]/voice"},
		{"esp webhook", "/webhooks/email/whsec_abc123", "/webhooks/email/[REDACTED]"},
		{"export download", "/exports/8f14e45f-ea8b-4b41-9c1a-2b5d0e6c1234", "/exports/[REDACTED]"},
		// Public by design: the survey id must stay readable, or the
		// request log stops being able to say which survey was hit.
		{"survey share link", "/s/abc123", "/s/abc123"},
		{"survey voice socket", "/s/abc123/voice", "/s/abc123/voice"},
		{"unrelated route", "/surveys/abc123/results", "/surveys/abc123/results"},
		// Prefixes only match at the credential position.
		{"prefix without a token", "/p", "/p"},
		{"prefix with an empty token", "/p/", "/p/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got := logging.ScrubURL(u); got != tc.want {
				t.Errorf("ScrubURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestScrubURL_RedactsPathAndQueryTogether(t *testing.T) {
	u, err := url.Parse("/p/inv_abc123?token=xyz789&lang=nl")
	if err != nil {
		t.Fatal(err)
	}
	got := logging.ScrubURL(u)
	if strings.Contains(got, "inv_abc123") {
		t.Errorf("ScrubURL result still contains the invite token: %s", got)
	}
	if strings.Contains(got, "xyz789") {
		t.Errorf("ScrubURL result still contains the query token: %s", got)
	}
	if !strings.Contains(got, "lang=nl") {
		t.Errorf("ScrubURL result dropped a non-sensitive param: %s", got)
	}
}

func TestScrubURL_Nil(t *testing.T) {
	if got := logging.ScrubURL(nil); got != "" {
		t.Errorf("ScrubURL(nil) = %q, want empty", got)
	}
}
