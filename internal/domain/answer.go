package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// AnswerValue is one respondent's answer to one question. It is stored as
// jsonb with exactly one field populated for the question's type, so the
// results layer (M7) can aggregate without re-deriving what shape to
// expect.
type AnswerValue struct {
	Text    string   `json:"text,omitempty"`
	Choice  string   `json:"choice,omitempty"`
	Choices []string `json:"choices,omitempty"`
	Number  *int     `json:"number,omitempty"`
	Bool    *bool    `json:"bool,omitempty"`
}

// IsEmpty reports whether the respondent left the question unanswered.
func (v AnswerValue) IsEmpty() bool {
	return strings.TrimSpace(v.Text) == "" && v.Choice == "" && len(v.Choices) == 0 &&
		v.Number == nil && v.Bool == nil
}

// Display renders an answer for a human reader (results, exports).
func (v AnswerValue) Display() string {
	switch {
	case v.Text != "":
		return v.Text
	case v.Choice != "":
		return v.Choice
	case len(v.Choices) > 0:
		return strings.Join(v.Choices, ", ")
	case v.Number != nil:
		return strconv.Itoa(*v.Number)
	case v.Bool != nil:
		if *v.Bool {
			return "Yes"
		}
		return "No"
	}
	return ""
}

// Encode serializes an answer for storage.
func (v AnswerValue) Encode() ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("domain: encode answer: %w", err)
	}
	return b, nil
}

// ParseAnswerValue decodes a stored answer.
func ParseAnswerValue(raw []byte) (AnswerValue, error) {
	if len(raw) == 0 {
		return AnswerValue{}, nil
	}
	var v AnswerValue
	if err := json.Unmarshal(raw, &v); err != nil {
		return AnswerValue{}, fmt.Errorf("domain: parse answer: %w", err)
	}
	return v, nil
}

// maxAnswerTextLen bounds a single free-text answer. Long enough for a
// thoughtful spoken answer transcribed in full, short enough that a bot
// cannot use the endpoint as free storage.
const maxAnswerTextLen = 10_000

// ErrRequiredAnswer and friends are shown to respondents, so they are
// phrased as help, never as diagnostics.
var (
	ErrRequiredAnswer = errors.New("this question needs an answer")
	ErrAnswerTooLong  = fmt.Errorf("please keep the answer under %d characters", maxAnswerTextLen)
	ErrNotAnOption    = errors.New("choose one of the options offered")
	ErrOutOfRange     = errors.New("choose a value on the scale")
)

// ValidateAnswer checks one answer against the question as it was asked.
// It runs on submission against the version the respondent was served, so
// a survey republished mid-fill cannot invalidate an in-flight answer.
func ValidateAnswer(q Question, v AnswerValue) error {
	if v.IsEmpty() {
		if q.Required {
			return ErrRequiredAnswer
		}
		return nil
	}

	switch q.Type {
	case LongText, ShortText:
		if len([]rune(v.Text)) > maxAnswerTextLen {
			return ErrAnswerTooLong
		}
	case SingleChoice, Dropdown:
		if !containsOption(q.Options, v.Choice) {
			return ErrNotAnOption
		}
	case MultipleChoice:
		for _, choice := range v.Choices {
			if !containsOption(q.Options, choice) {
				return ErrNotAnOption
			}
		}
	case RatingScale, NPS:
		if v.Number == nil {
			return ErrOutOfRange
		}
		min, max := q.Scale()
		if *v.Number < min || *v.Number > max {
			return ErrOutOfRange
		}
	case YesNo:
		if v.Bool == nil {
			return ErrRequiredAnswer
		}
	default:
		return ErrUnknownType
	}
	return nil
}

func containsOption(options []string, want string) bool {
	for _, opt := range options {
		if opt == want {
			return true
		}
	}
	return false
}

// Submission is a complete set of answers keyed by Question Identity,
// validated together so a respondent sees every problem at once rather
// than one per round trip.
type Submission struct {
	Answers map[string]AnswerValue
}

// AnswerError names the question a problem belongs to, so the renderer can
// place the message beside it.
type AnswerError struct {
	IdentityID string
	Position   int
	Message    string
}

// Validate checks a whole submission against the questions as served.
// Answers to questions that are not in this version are ignored rather
// than rejected: a stale form field is the respondent's browser being out
// of date, not something to punish them for.
func (s Submission) Validate(questions []Question) []AnswerError {
	var problems []AnswerError
	for i, q := range questions {
		if err := ValidateAnswer(q, s.Answers[q.IdentityID]); err != nil {
			problems = append(problems, AnswerError{
				IdentityID: q.IdentityID,
				Position:   i + 1,
				Message:    err.Error(),
			})
		}
	}
	return problems
}
