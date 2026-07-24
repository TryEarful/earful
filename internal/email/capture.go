package email

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Capture is the test fake (SPEC.md Testing Decisions): it records every
// message so application-edge tests can read magic links and invites the
// way a user reads their inbox. Its webhook accepts the same event shape
// as Brevo's, so suppression flows are testable end to end.
type Capture struct {
	mu      sync.Mutex
	msgs    []Message
	failFor map[string]bool

	onEvent EventHandler
}

func NewCapture() *Capture { return &Capture{failFor: map[string]bool{}} }

func (c *Capture) SetEventHandler(h EventHandler) { c.onEvent = h }

// FailFor makes future sends to addr error, simulating a provider
// refusing an address mid-batch; Recover undoes it.
func (c *Capture) FailFor(addr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failFor[addr] = true
}

func (c *Capture) Recover(addr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.failFor, addr)
}

func (c *Capture) Send(_ context.Context, msg Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failFor[msg.To] {
		return errors.New("email: capture: simulated send failure")
	}
	c.msgs = append(c.msgs, msg)
	return nil
}

func (c *Capture) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	var event struct {
		Event string `json:"event"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&event); err != nil {
		http.Error(w, "unparseable event", http.StatusBadRequest)
		return
	}
	address := strings.ToLower(strings.TrimSpace(event.Email))
	if reason, ok := suppressingEvents[event.Event]; ok && address != "" && c.onEvent != nil {
		c.onEvent(r.Context(), address, reason)
	}
	w.WriteHeader(http.StatusOK)
}

// All returns a copy of every captured message.
func (c *Capture) All() []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Message(nil), c.msgs...)
}

// Last returns the most recently captured message, or false if none.
func (c *Capture) Last() (Message, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.msgs) == 0 {
		return Message{}, false
	}
	return c.msgs[len(c.msgs)-1], true
}

// To returns all messages sent to addr.
func (c *Capture) To(addr string) []Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Message
	for _, m := range c.msgs {
		if m.To == addr {
			out = append(out, m)
		}
	}
	return out
}
