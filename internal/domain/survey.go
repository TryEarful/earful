package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Draft is the mutable working copy of a survey's structure. It is stored
// as one jsonb document because it is always read and written whole;
// publishing is what turns it into relational, immutable rows.
type Draft struct {
	Questions []Question `json:"questions"`
}

// maxQuestionsPerSurvey bounds a single survey. A survey this long is a
// different product; the cap also keeps the publish transaction small.
const maxQuestionsPerSurvey = 100

var (
	ErrDraftEmpty    = errors.New("add at least one question before publishing")
	ErrDraftTooLong  = fmt.Errorf("a survey can hold at most %d questions", maxQuestionsPerSurvey)
	ErrQuestionUnknown = errors.New("that question is not part of this draft")
)

// ParseDraft decodes a stored draft document.
func ParseDraft(raw []byte) (Draft, error) {
	if len(raw) == 0 {
		return Draft{}, nil
	}
	var d Draft
	if err := json.Unmarshal(raw, &d); err != nil {
		return Draft{}, fmt.Errorf("domain: parse draft: %w", err)
	}
	return d, nil
}

// Encode serializes the draft for storage.
func (d Draft) Encode() ([]byte, error) {
	if d.Questions == nil {
		d.Questions = []Question{}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("domain: encode draft: %w", err)
	}
	return b, nil
}

// IndexOf returns the position of the question with the given identity.
func (d Draft) IndexOf(identityID string) (int, bool) {
	for i, q := range d.Questions {
		if q.IdentityID == identityID {
			return i, true
		}
	}
	return 0, false
}

// Add appends a validated question.
func (d *Draft) Add(q Question) error {
	if len(d.Questions) >= maxQuestionsPerSurvey {
		return ErrDraftTooLong
	}
	if err := q.Validate(); err != nil {
		return err
	}
	d.Questions = append(d.Questions, q)
	return nil
}

// Replace updates the question carrying identityID in place. The identity
// is preserved no matter how much the wording changes — that is precisely
// what makes results comparable across versions (ADR-0001).
func (d *Draft) Replace(identityID string, q Question) error {
	i, ok := d.IndexOf(identityID)
	if !ok {
		return ErrQuestionUnknown
	}
	q.IdentityID = identityID
	if err := q.Validate(); err != nil {
		return err
	}
	d.Questions[i] = q
	return nil
}

// Remove drops a question from the draft. Its Question Identity simply
// stops appearing in future versions; nothing that references it is
// touched, so past responses keep their meaning.
func (d *Draft) Remove(identityID string) error {
	i, ok := d.IndexOf(identityID)
	if !ok {
		return ErrQuestionUnknown
	}
	d.Questions = append(d.Questions[:i], d.Questions[i+1:]...)
	return nil
}

// Move shifts a question by delta positions, clamping at the ends so the
// buttons are always safe to press.
func (d *Draft) Move(identityID string, delta int) error {
	i, ok := d.IndexOf(identityID)
	if !ok {
		return ErrQuestionUnknown
	}
	j := i + delta
	if j < 0 {
		j = 0
	}
	if j > len(d.Questions)-1 {
		j = len(d.Questions) - 1
	}
	if i == j {
		return nil
	}
	q := d.Questions[i]
	d.Questions = append(d.Questions[:i], d.Questions[i+1:]...)
	rest := append([]Question{q}, d.Questions[j:]...)
	d.Questions = append(d.Questions[:j], rest...)
	return nil
}

// ValidateForPublish re-checks every question at publish time, so a rule
// added after a draft was written still gates the version that draft
// becomes.
func (d Draft) ValidateForPublish() error {
	if len(d.Questions) == 0 {
		return ErrDraftEmpty
	}
	if len(d.Questions) > maxQuestionsPerSurvey {
		return ErrDraftTooLong
	}
	for i, q := range d.Questions {
		if err := q.Validate(); err != nil {
			return fmt.Errorf("question %d: %w", i+1, err)
		}
	}
	return nil
}

// Status is the Survey Status a creator sees (SPEC.md story 14). It is
// derived from facts, never stored, so it cannot drift out of agreement
// with them.
type Status string

const (
	StatusDraft  Status = "Draft"
	StatusOpen   Status = "Open"
	StatusClosed Status = "Closed"
)

// SurveyState is the minimum needed to derive a status.
type SurveyState struct {
	HasPublishedVersion bool
	CloseAt             *time.Time
	ClosedAt            *time.Time
}

// StatusAt derives the status as of now: never published is a Draft;
// manually closed or past its Close Date is Closed; anything else is Open
// and accepting responses.
func (s SurveyState) StatusAt(now time.Time) Status {
	if !s.HasPublishedVersion {
		return StatusDraft
	}
	if s.ClosedAt != nil {
		return StatusClosed
	}
	if s.CloseAt != nil && !now.Before(*s.CloseAt) {
		return StatusClosed
	}
	return StatusOpen
}

// AcceptsResponses is the question the respondent path actually asks.
func (s SurveyState) AcceptsResponses(now time.Time) bool {
	return s.StatusAt(now) == StatusOpen
}

// maxTitleLen bounds the survey title.
const maxTitleLen = 200

var ErrEmptyTitle = errors.New("give the survey a title")

// ValidateTitle checks a survey title.
func ValidateTitle(title string) error {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return ErrEmptyTitle
	}
	if len([]rune(trimmed)) > maxTitleLen {
		return fmt.Errorf("keep the title under %d characters", maxTitleLen)
	}
	return nil
}
