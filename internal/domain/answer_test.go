package domain_test

import (
	"errors"
	"testing"

	"github.com/TryEarful/earful/internal/domain"
)

func TestDateQuestion_Traits(t *testing.T) {
	t.Parallel()
	if domain.Date.AcceptsVoice() {
		t.Error("a date question must not offer voice (ADR-0004: voice is for open text)")
	}
	if domain.Date.NeedsOptions() || domain.Date.NeedsScale() {
		t.Error("a date question carries neither options nor a scale")
	}
	q := domain.Question{Type: domain.Date, Text: "When did you visit?"}
	if err := q.Validate(); err != nil {
		t.Errorf("a date question was refused: %v", err)
	}
}

// TestValidateAnswer_Date: a date answer is accepted only as a real
// calendar day in yyyy-mm-dd, the form a browser's date control submits.
func TestValidateAnswer_Date(t *testing.T) {
	t.Parallel()
	required := domain.Question{IdentityID: "d", Type: domain.Date, Text: "When?", Required: true}
	optional := domain.Question{IdentityID: "d", Type: domain.Date, Text: "When?"}

	cases := []struct {
		name string
		q    domain.Question
		v    domain.AnswerValue
		want error
	}{
		{"a day", required, domain.AnswerValue{Date: "2026-04-18"}, nil},
		{"leap day", required, domain.AnswerValue{Date: "2028-02-29"}, nil},
		{"skipped optional", optional, domain.AnswerValue{}, nil},
		{"skipped required", required, domain.AnswerValue{}, domain.ErrRequiredAnswer},
		{"blank required", required, domain.AnswerValue{Date: "   "}, domain.ErrRequiredAnswer},
		{"no such day", required, domain.AnswerValue{Date: "2026-02-30"}, domain.ErrNotADate},
		{"not a leap year", required, domain.AnswerValue{Date: "2027-02-29"}, domain.ErrNotADate},
		{"no such month", required, domain.AnswerValue{Date: "2026-13-01"}, domain.ErrNotADate},
		{"day first", required, domain.AnswerValue{Date: "18/04/2026"}, domain.ErrNotADate},
		{"unpadded", required, domain.AnswerValue{Date: "2026-4-18"}, domain.ErrNotADate},
		{"with a time", required, domain.AnswerValue{Date: "2026-04-18T10:00:00Z"}, domain.ErrNotADate},
		{"words", optional, domain.AnswerValue{Date: "last spring"}, domain.ErrNotADate},
		{"text in place of a date", optional, domain.AnswerValue{Text: "2026-04-18"}, domain.ErrNotADate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := domain.ValidateAnswer(tc.q, tc.v)
			if !errors.Is(err, tc.want) {
				t.Errorf("ValidateAnswer(%+v) = %v, want %v", tc.v, err, tc.want)
			}
		})
	}
}

func TestAnswerValue_DateRoundTrip(t *testing.T) {
	t.Parallel()
	v := domain.AnswerValue{Date: "2026-04-18"}
	if got := v.Display(); got != "2026-04-18" {
		t.Errorf("Display() = %q, want the ISO date", got)
	}
	raw, err := v.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"date":"2026-04-18"}` {
		t.Errorf("stored as %s, want only the date field", raw)
	}
	back, err := domain.ParseAnswerValue(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Date != v.Date || back.IsEmpty() {
		t.Errorf("round trip = %+v, want %+v", back, v)
	}
}
