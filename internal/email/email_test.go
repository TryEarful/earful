package email_test

import (
	"context"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/email"
)

func TestConsole_WritesMessage(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	c := email.NewConsole(&out)

	if err := c.Send(context.Background(), email.Message{
		To:      "someone@example.test",
		Subject: "Your Earful sign-in link",
		Text:    "https://example.test/auth/magic/verify?token=abc",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	got := out.String()
	for _, want := range []string{"someone@example.test", "Your Earful sign-in link", "token=abc"} {
		if !strings.Contains(got, want) {
			t.Errorf("console output missing %q:\n%s", want, got)
		}
	}
}

func TestCapture_RecordsAndFilters(t *testing.T) {
	t.Parallel()
	c := email.NewCapture()
	ctx := context.Background()

	if _, ok := c.Last(); ok {
		t.Error("empty capture reported a last message")
	}

	c.Send(ctx, email.Message{To: "a@example.test", Subject: "first"})  //nolint:errcheck
	c.Send(ctx, email.Message{To: "b@example.test", Subject: "second"}) //nolint:errcheck
	c.Send(ctx, email.Message{To: "a@example.test", Subject: "third"})  //nolint:errcheck

	if got := len(c.All()); got != 3 {
		t.Errorf("All() = %d messages, want 3", got)
	}
	if got := len(c.To("a@example.test")); got != 2 {
		t.Errorf("To(a) = %d messages, want 2", got)
	}
	last, ok := c.Last()
	if !ok || last.Subject != "third" {
		t.Errorf("Last() = %+v, want the \"third\" message", last)
	}
}
