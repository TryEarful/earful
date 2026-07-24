// Package email defines the two-method Sender interface behind which all
// outbound mail lives (ADR-0005). M2 ships the Console implementation
// (local dev / staging bootstrap) and the Capture fake for tests; the
// Brevo and SMTP implementations arrive with M4-T6.
package email

import (
	"context"
	"net/http"
)

// Message is one outbound email. Text-only for now; HTML can be added as
// an optional field when a template needs it.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Sender is the seam ADR-0005 defines: Send delivers a message,
// HandleWebhook ingests provider callbacks (bounces, complaints) to feed
// the suppression list. Implementations without webhooks (console, SMTP)
// respond 404.
type Sender interface {
	Send(ctx context.Context, msg Message) error
	HandleWebhook(w http.ResponseWriter, r *http.Request)
}
