package email

import (
	"context"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
)

// SMTP delivers through any SMTP relay — mailpit in local dev, the
// self-hoster's own server in production (Appendix D). Auth is optional:
// mailpit needs none, real relays get PLAIN.
type SMTP struct {
	Addr string // host:port
	From string
	User string // empty = no auth
	Pass string
}

func (s *SMTP) Send(_ context.Context, msg Message) error {
	var auth smtp.Auth
	if s.User != "" {
		host, _, _ := strings.Cut(s.Addr, ":")
		auth = smtp.PlainAuth("", s.User, s.Pass, host)
	}
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n",
		s.From, msg.To, msg.Subject, msg.Text)
	if err := smtp.SendMail(s.Addr, auth, s.From, []string{msg.To}, []byte(body)); err != nil {
		return fmt.Errorf("email: smtp send: %w", err)
	}
	return nil
}

// HandleWebhook: plain SMTP has no event feed; bounces land in a mailbox
// nobody reads. Self-hosters who need suppression feedback configure
// Brevo (or wait for the post-MVP integrations).
func (s *SMTP) HandleWebhook(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "no webhook for smtp sender", http.StatusNotFound)
}
