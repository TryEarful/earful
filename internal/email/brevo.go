package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// EventHandler receives bounce/complaint events so the application can
// feed the suppression list. The reason is a short slug ("hard_bounce",
// "complaint").
type EventHandler func(ctx context.Context, address, reason string)

// EventSender is a Sender whose webhook produces suppression events. The
// HTTP layer wires its handler at startup when the configured sender
// supports it.
type EventSender interface {
	Sender
	SetEventHandler(EventHandler)
}

// Brevo sends through the Brevo transactional API (ADR-0005) and ingests
// its webhook events. The request/response shapes are unit-tested against
// a fake endpoint and were verified against the live v3 API and webhook
// documentation when production flipped to Brevo (M4-T6).
type Brevo struct {
	APIKey  string
	From    string
	BaseURL string // override for tests; default the real API
	Client  *http.Client

	onEvent EventHandler
}

const brevoDefaultBaseURL = "https://api.brevo.com/v3"

func (b *Brevo) SetEventHandler(h EventHandler) { b.onEvent = h }

func (b *Brevo) Send(ctx context.Context, msg Message) error {
	baseURL := b.BaseURL
	if baseURL == "" {
		baseURL = brevoDefaultBaseURL
	}
	client := b.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	payload, err := json.Marshal(map[string]any{
		"sender":      map[string]string{"email": b.From},
		"to":          []map[string]string{{"email": msg.To}},
		"subject":     msg.Subject,
		"textContent": msg.Text,
	})
	if err != nil {
		return fmt.Errorf("email: encode brevo payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/smtp/email", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("email: build brevo request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", b.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("email: brevo send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("email: brevo send: status %d: %s", resp.StatusCode, body)
	}
	return nil
}

// brevoEvent is the slice of Brevo's webhook body we act on. Parsing is
// deliberately lenient: unknown fields and unknown events are ignored,
// because the webhook must never bounce (pun intended) on a payload
// variation.
type brevoEvent struct {
	Event string `json:"event"`
	Email string `json:"email"`
}

// suppressingEvents maps Brevo webhook payload event slugs to
// suppression reasons. The live transactional payloads use hard_bounce,
// spam, invalid_email, blocked and unsubscribed; the extra keys are
// doc-era aliases kept because the handler is deliberately lenient and
// an unused key costs nothing. Soft bounces are excluded on purpose — a
// full mailbox is not a dead address.
var suppressingEvents = map[string]string{
	"hard_bounce":   "hard_bounce",
	"spam":          "complaint",
	"complaint":     "complaint",
	"invalid":       "invalid_address",
	"invalid_email": "invalid_address",
	"blocked":       "blocked",
	"unsubscribe":   "unsubscribed",
	"unsubscribed":  "unsubscribed",
}

func (b *Brevo) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	var event brevoEvent
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&event); err != nil {
		http.Error(w, "unparseable event", http.StatusBadRequest)
		return
	}
	address := strings.ToLower(strings.TrimSpace(event.Email))
	if reason, ok := suppressingEvents[event.Event]; ok && address != "" && b.onEvent != nil {
		b.onEvent(r.Context(), address, reason)
	}
	w.WriteHeader(http.StatusOK)
}
