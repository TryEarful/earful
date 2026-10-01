package domain_test

import (
	"errors"
	"testing"

	"github.com/TryEarful/earful/internal/domain"
)

func TestNumberQuestion_Traits(t *testing.T) {
	t.Parallel()
	if domain.Number.AcceptsVoice() || domain.Number.NeedsOptions() {
		t.Error("a number question offers neither voice nor options")
	}
	if domain.Number.NeedsScale() || !domain.Number.HasBounds() {
		t.Error("a number question carries limits, not a rating scale")
	}
	if !domain.RatingScale.HasBounds() || domain.NPS.HasBounds() || domain.Date.HasBounds() {
		t.Error("only rating scales and numbers carry limits the author sets")
	}
	q := domain.Question{Type: domain.Number, Text: "How many came?", ScaleMin: 1, ScaleMax: 12}
	if min, max := q.Scale(); min != 1 || max != 12 {
		t.Errorf("Scale() = %d to %d, want the question's own 1 to 12", min, max)
	}
	if points := q.ScalePoints(); points != nil {
		t.Errorf("a number question has no points to pick, got %v", points)
	}
	// No 1 to 5 fallback: a number whose limits are unusable is refused,
	// never quietly narrowed to a rating's default.
	boundless := domain.Question{Type: domain.Number, Text: "How many?"}
	if min, max := boundless.Scale(); min != 0 || max != 0 {
		t.Errorf("Scale() of a boundless number = %d to %d, want its own 0 to 0", min, max)
	}
	if err := boundless.Validate(); !errors.Is(err, domain.ErrBadBounds) {
		t.Errorf("a boundless number was not refused: %v", err)
	}
}

// TestNumberQuestion_Bounds: the lowest answer is below the highest, and
// both are within a million of zero.
func TestNumberQuestion_Bounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		min, max int
		want     error
	}{
		{"one to twelve", 1, 12, nil},
		{"from zero", 0, 100, nil},
		{"below zero", -40, 50, nil},
		{"the widest", -domain.NumberBoundLimit, domain.NumberBoundLimit, nil},
		{"wider than a rating", 0, 1000, nil},
		{"equal", 5, 5, domain.ErrBadBounds},
		{"inverted", 12, 1, domain.ErrBadBounds},
		{"both missing", 0, 0, domain.ErrBadBounds},
		{"too high", 0, domain.NumberBoundLimit + 1, domain.ErrBadBounds},
		{"too low", -domain.NumberBoundLimit - 1, 0, domain.ErrBadBounds},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := domain.Question{Type: domain.Number, Text: "How many?", ScaleMin: tc.min, ScaleMax: tc.max}
			if err := q.Validate(); !errors.Is(err, tc.want) {
				t.Errorf("Validate(%d to %d) = %v, want %v", tc.min, tc.max, err, tc.want)
			}
		})
	}
	// A rating keeps its own rules: a number's range does not widen it.
	wide := domain.Question{Type: domain.RatingScale, Text: "Rate", ScaleMin: 1, ScaleMax: 12}
	if err := wide.Validate(); !errors.Is(err, domain.ErrBadScale) {
		t.Errorf("a 1 to 12 rating was not refused: %v", err)
	}
}

// TestValidateAnswer_Number: a number answer is a whole number within the
// question's limits. Anything typed that is not one arrives as text and
// is refused with the limits, not taken for a skip.
func TestValidateAnswer_Number(t *testing.T) {
	t.Parallel()
	required := domain.Question{IdentityID: "n", Type: domain.Number, Text: "How many?", Required: true, ScaleMin: 1, ScaleMax: 12}
	optional := domain.Question{IdentityID: "n", Type: domain.Number, Text: "How many?", ScaleMin: -5, ScaleMax: 5}
	n := func(v int) *int { return &v }
	outside := domain.RangeError{Min: 1, Max: 12}

	cases := []struct {
		name string
		q    domain.Question
		v    domain.AnswerValue
		want error
	}{
		{"the lowest", required, domain.AnswerValue{Number: n(1)}, nil},
		{"the highest", required, domain.AnswerValue{Number: n(12)}, nil},
		{"between", required, domain.AnswerValue{Number: n(7)}, nil},
		{"below zero", optional, domain.AnswerValue{Number: n(-5)}, nil},
		{"zero", optional, domain.AnswerValue{Number: n(0)}, nil},
		{"skipped optional", optional, domain.AnswerValue{}, nil},
		{"skipped required", required, domain.AnswerValue{}, domain.ErrRequiredAnswer},
		{"below", required, domain.AnswerValue{Number: n(0)}, outside},
		{"above", required, domain.AnswerValue{Number: n(13)}, outside},
		{"a fraction", required, domain.AnswerValue{Text: "2.5"}, outside},
		{"words", required, domain.AnswerValue{Text: "a dozen"}, outside},
		{"a fraction on an optional question", optional, domain.AnswerValue{Text: "1.5"}, domain.RangeError{Min: -5, Max: 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateAnswer(tc.q, tc.v)
			if !errors.Is(err, tc.want) {
				t.Errorf("ValidateAnswer(%+v) = %v, want %v", tc.v, err, tc.want)
			}
		})
	}
	if got := outside.Error(); got != "enter a whole number from 1 to 12" {
		t.Errorf("RangeError reads %q", got)
	}
	wide := domain.RangeError{Min: -domain.NumberBoundLimit, Max: domain.NumberBoundLimit}
	if got := wide.Error(); got != "enter a whole number from -1,000,000 to 1,000,000" {
		t.Errorf("RangeError reads %q", got)
	}
}
