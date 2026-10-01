package domain

import (
	"errors"
	"net/url"
	"strings"
)

// ThankYou is what a respondent reads after sending their answers, in
// place of the default text, and an optional link onward. It is part of
// the draft and is frozen into each published version with the
// questions, because what a respondent is shown after answering is as
// much a part of the survey they answered as the questions were
// (ADR-0001). The zero value means "not set": the page then shows the
// default text, exactly as it did before the creator could write one.
type ThankYou struct {
	Message   string `json:"message,omitempty"`
	LinkLabel string `json:"link_label,omitempty"`
	LinkURL   string `json:"link_url,omitempty"`
}

const (
	maxThanksMessageLen = 1000
	maxThanksLabelLen   = 60
	// maxThanksURLLen is far above any address a person types, and well
	// below what a browser or a proxy refuses.
	maxThanksURLLen = 2000
)

var (
	// ErrThanksLinkURL refuses anything but an absolute http or https
	// address. A javascript: or data: URL would run in the respondent's
	// page under Earful's origin, and a relative one would point into
	// Earful itself, which is not the creator's to link to.
	ErrThanksLinkURL = errors.New("the link address must start with http:// or https:// and name a website")
	// ErrThanksLinkLabel: a bare address tells a respondent nothing
	// about where it goes.
	ErrThanksLinkLabel = errors.New("give the link a label, so people know where it goes")
	// ErrThanksLinkAddress: a label with nowhere to go is a dead link.
	ErrThanksLinkAddress = errors.New("add the address the link goes to, or clear its label")
)

// NewThankYou builds a ThankYou from what a creator typed, trimmed and
// with line endings made uniform, and checks it.
func NewThankYou(message, linkLabel, linkURL string) (ThankYou, error) {
	t := ThankYou{
		Message:   normalizeMessage(message),
		LinkLabel: strings.TrimSpace(linkLabel),
		LinkURL:   strings.TrimSpace(linkURL),
	}
	if err := t.Validate(); err != nil {
		return t, err
	}
	return t, nil
}

// IsZero reports whether nothing is set, so the default text is shown.
func (t ThankYou) IsZero() bool { return t == ThankYou{} }

// HasLink reports whether a link is shown.
func (t ThankYou) HasLink() bool { return t.LinkURL != "" }

// Validate checks the lengths, and that a link has both a label and an
// address a browser will open as a web page.
func (t ThankYou) Validate() error {
	if len([]rune(t.Message)) > maxThanksMessageLen {
		return LimitError{Kind: LimitThanksMessage, Limit: maxThanksMessageLen}
	}
	if len([]rune(t.LinkLabel)) > maxThanksLabelLen {
		return LimitError{Kind: LimitThanksLabel, Limit: maxThanksLabelLen}
	}
	switch {
	case t.LinkURL == "" && t.LinkLabel == "":
		return nil
	case t.LinkURL == "":
		return ErrThanksLinkAddress
	case t.LinkLabel == "":
		return ErrThanksLinkLabel
	}
	return ValidateLinkURL(t.LinkURL)
}

// ValidateLinkURL accepts an absolute http or https address with a host
// and no user name or password in it. Credentials in an address are a
// classic way to make one site's link read as another's.
func ValidateLinkURL(raw string) error {
	if len(raw) > maxThanksURLLen {
		return ErrThanksLinkURL
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return ErrThanksLinkURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrThanksLinkURL
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ErrThanksLinkURL
	}
	if u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return ErrThanksLinkURL
	}
	return nil
}

// Paragraphs splits a message as its writer laid it out: a blank line
// starts a paragraph, a single line break stays a line break. The text
// itself is never interpreted, so a creator cannot put markup on a
// respondent's page.
func (t ThankYou) Paragraphs() [][]string {
	return paragraphs(t.Message)
}

func paragraphs(message string) [][]string {
	var out [][]string
	var current []string
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				out = append(out, current)
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

// normalizeMessage makes line endings uniform and trims the message, so
// that the same words typed in two browsers are the same message.
func normalizeMessage(message string) string {
	message = strings.ReplaceAll(message, "\r\n", "\n")
	message = strings.ReplaceAll(message, "\r", "\n")
	return strings.TrimSpace(message)
}

// SetThanks replaces the draft's thank you page.
func (d *Draft) SetThanks(t ThankYou) error {
	if err := t.Validate(); err != nil {
		return err
	}
	d.Thanks = t
	return nil
}
