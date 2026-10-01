package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
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
	// Date is a calendar day in ISO 8601 form (DateLayout), with no time
	// and no zone: the day the respondent picked, not an instant.
	Date string `json:"date,omitempty"`
	// Other is what the respondent wrote in the box beside Other, on a
	// question that offers it. It is set exactly when Choice is, or
	// Choices holds, OtherChoice.
	Other string `json:"other,omitempty"`
}

// OtherChoice is the choice an answer records when the respondent picked
// Other, and the value the Other control posts. Options are compared by
// their text, so the marker is spelled as no option a person writes, and
// a question that offers Other refuses an option spelled like it
// (ErrReservedOption). What the respondent wrote is kept in Other, never
// in the choice, so it cannot be mistaken for an option either.
const OtherChoice = "__other__"

// DateLayout is the only form a date answer is accepted and stored in. It
// is what a browser's date control submits, whatever the respondent's
// locale shows them, and it sorts as text in calendar order.
const DateLayout = "2006-01-02"

// IsEmpty reports whether the respondent left the question unanswered.
func (v AnswerValue) IsEmpty() bool {
	return strings.TrimSpace(v.Text) == "" && v.Choice == "" && len(v.Choices) == 0 &&
		v.Number == nil && v.Bool == nil && strings.TrimSpace(v.Date) == "" &&
		strings.TrimSpace(v.Other) == ""
}

// Display renders an answer for a human reader (results, exports). An
// Other answer reads "Other: " and what was written.
func (v AnswerValue) Display() string {
	return v.DisplayWith(func(written string) string { return "Other: " + written })
}

// DisplayWith renders an answer as Display does, except that the
// function given words an Other answer, so a reader can be shown it in
// their own language.
func (v AnswerValue) DisplayWith(other func(written string) string) string {
	choice := func(c string) string {
		if c == OtherChoice && v.Other != "" {
			return other(v.Other)
		}
		return c
	}
	switch {
	case v.Text != "":
		return v.Text
	case v.Choice != "":
		return choice(v.Choice)
	case len(v.Choices) > 0:
		shown := make([]string, len(v.Choices))
		for i, c := range v.Choices {
			shown[i] = choice(c)
		}
		return strings.Join(shown, ", ")
	case v.Number != nil:
		return strconv.Itoa(*v.Number)
	case v.Bool != nil:
		if *v.Bool {
			return "Yes"
		}
		return "No"
	case v.Date != "":
		return v.Date
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
	ErrAnswerTooLong  = error(LimitError{Kind: LimitAnswerText, Limit: maxAnswerTextLen})
	ErrNotAnOption    = errors.New("choose one of the options offered")
	ErrOutOfRange     = errors.New("choose a value on the scale")
	ErrNotADate       = errors.New("enter a date as year, month and day")
	ErrOtherEmpty     = errors.New("write your answer in the box beside Other")
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

	if v.Other != "" && !q.AllowOther {
		return ErrNotAnOption
	}
	if len([]rune(v.Other)) > maxAnswerTextLen {
		return ErrAnswerTooLong
	}

	switch q.Type {
	case LongText, ShortText:
		if len([]rune(v.Text)) > maxAnswerTextLen {
			return ErrAnswerTooLong
		}
	case SingleChoice, Dropdown:
		if q.AllowOther && v.Choice == OtherChoice {
			if strings.TrimSpace(v.Other) == "" {
				return ErrOtherEmpty
			}
			return nil
		}
		if v.Other != "" || !containsOption(q.Options, v.Choice) {
			return ErrNotAnOption
		}
	case MultipleChoice:
		other := false
		for _, choice := range v.Choices {
			if q.AllowOther && choice == OtherChoice {
				other = true
				continue
			}
			if !containsOption(q.Options, choice) {
				return ErrNotAnOption
			}
		}
		switch {
		case other && strings.TrimSpace(v.Other) == "":
			return ErrOtherEmpty
		case !other && v.Other != "":
			return ErrNotAnOption
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
	case Date:
		if _, err := time.Parse(DateLayout, v.Date); err != nil {
			return ErrNotADate
		}
	case Number:
		// Something typed that does not read as a whole number arrives
		// as Text, so it is refused here rather than taken for a skip.
		min, max := q.Scale()
		if v.Number == nil || *v.Number < min || *v.Number > max {
			return RangeError{Min: min, Max: max}
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
	// Err is the problem, and Message is Err in English. Whoever shows
	// the problem to a respondent words it from Err.
	Err     error
	Message string
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
				Err:        err,
				Message:    err.Error(),
			})
		}
	}
	return problems
}
