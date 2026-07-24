package email_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/email"
)

// TestBrevo_WebhookEvents pins the webhook handler to Brevo's live
// transactional payload slugs: every suppressing event reaches the
// handler with its reason and a normalized address, while soft bounces
// and deliveries never suppress.
func TestBrevo_WebhookEvents(t *testing.T) {
	t.Parallel()
	cases := []struct {
		event  string
		reason string // "" = must not suppress
	}{
		{"hard_bounce", "hard_bounce"},
		{"spam", "complaint"},
		{"invalid_email", "invalid_address"},
		{"blocked", "blocked"},
		{"unsubscribed", "unsubscribed"},
		{"soft_bounce", ""},
		{"delivered", ""},
	}
	for _, tc := range cases {
		t.Run(tc.event, func(t *testing.T) {
			t.Parallel()
			b := &email.Brevo{}
			var gotAddr, gotReason string
			b.SetEventHandler(func(_ context.Context, address, reason string) {
				gotAddr, gotReason = address, reason
			})

			body := `{"event":"` + tc.event + `","email":" Bouncer@Example.Test "}`
			rec := httptest.NewRecorder()
			b.HandleWebhook(rec, httptest.NewRequest(http.MethodPost, "/webhooks/email/secret", strings.NewReader(body)))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if tc.reason == "" {
				if gotReason != "" {
					t.Errorf("event %q suppressed with reason %q, want no suppression", tc.event, gotReason)
				}
				return
			}
			if gotReason != tc.reason {
				t.Errorf("reason = %q, want %q", gotReason, tc.reason)
			}
			if gotAddr != "bouncer@example.test" {
				t.Errorf("address = %q, want normalized %q", gotAddr, "bouncer@example.test")
			}
		})
	}
}
