package email

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Console writes messages to w (stdout in dev). It deliberately bypasses
// the slog scrubbing pipeline: magic-link emails ARE credentials, and
// printing them is the console sender's entire purpose — it is how a
// developer without an ESP completes a login. It must never be configured
// in production once a real sender exists (M4-T6); when that config
// validation lands it MUST exempt APP_ENV=staging — staging runs the
// console sender on purpose so the post-deploy e2e smoke gate can read
// magic links back out of Cloud Logging (see docs/runbook.md).
type Console struct {
	mu sync.Mutex
	W  io.Writer
}

func NewConsole(w io.Writer) *Console { return &Console{W: w} }

func (c *Console) Send(_ context.Context, msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := fmt.Fprintf(c.W,
		"\n--- earful console email (dev only) ---\nTo: %s\nSubject: %s\n\n%s\n---------------------------------------\n",
		msg.To, msg.Subject, msg.Text)
	return err
}

func (c *Console) HandleWebhook(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "no webhook for console sender", http.StatusNotFound)
}
