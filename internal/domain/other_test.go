package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/TryEarful/earful/internal/domain"
)

// TestQuestion_ValidateAllowOther: Other is offered only on the types
// that list options, and a question offering it may not carry an option
// spelled like the marker, which would read back as Other.
func TestQuestion_ValidateAllowOther(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		q    domain.Question
		want error
	}{
		{"single choice", domain.Question{Type: domain.SingleChoice, Text: "Q", Options: []string{"a", "b"}, AllowOther: true}, nil},
		{"multiple choice", domain.Question{Type: domain.MultipleChoice, Text: "Q", Options: []string{"a", "b"}, AllowOther: true}, nil},
		{"dropdown", domain.Question{Type: domain.Dropdown, Text: "Q", Options: []string{"a", "b"}, AllowOther: true}, nil},
		{"long text", domain.Question{Type: domain.LongText, Text: "Q", AllowOther: true}, domain.ErrOtherNotOffered},
		{"yes or no", domain.Question{Type: domain.YesNo, Text: "Q", AllowOther: true}, domain.ErrOtherNotOffered},
		{"rating", domain.Question{Type: domain.RatingScale, Text: "Q", ScaleMin: 1, ScaleMax: 5, AllowOther: true}, domain.ErrOtherNotOffered},
		{"marker as an option", domain.Question{Type: domain.SingleChoice, Text: "Q",
			Options: []string{"a", strings.ToUpper(domain.OtherChoice)}, AllowOther: true}, domain.ErrReservedOption},
		{"marker as an option without Other", domain.Question{Type: domain.SingleChoice, Text: "Q",
			Options: []string{"a", domain.OtherChoice}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.q.Validate(); !errors.Is(err, tc.want) {
				t.Errorf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestValidateAnswer_Other: Other is accepted only where it is offered,
// and only with something written beside it.
func TestValidateAnswer_Other(t *testing.T) {
	t.Parallel()
	options := []string{"Email", "Phone"}
	single := domain.Question{IdentityID: "s", Type: domain.SingleChoice, Text: "How?", Options: options, AllowOther: true}
	required := single
	required.Required = true
	dropdown := domain.Question{IdentityID: "d", Type: domain.Dropdown, Text: "How?", Options: options, AllowOther: true}
	multiple := domain.Question{IdentityID: "m", Type: domain.MultipleChoice, Text: "How?", Options: options, AllowOther: true}
	without := domain.Question{IdentityID: "w", Type: domain.SingleChoice, Text: "How?", Options: options}
	withoutMultiple := domain.Question{IdentityID: "wm", Type: domain.MultipleChoice, Text: "How?", Options: options}
	long := strings.Repeat("x", 10_001)

	cases := []struct {
		name string
		q    domain.Question
		v    domain.AnswerValue
		want error
	}{
		{"single, other written", single, domain.AnswerValue{Choice: domain.OtherChoice, Other: "Carrier pigeon"}, nil},
		{"dropdown, other written", dropdown, domain.AnswerValue{Choice: domain.OtherChoice, Other: "Fax"}, nil},
		{"single, an option", single, domain.AnswerValue{Choice: "Email"}, nil},
		{"single, other left empty", single, domain.AnswerValue{Choice: domain.OtherChoice}, domain.ErrOtherEmpty},
		{"single, other only spaces", single, domain.AnswerValue{Choice: domain.OtherChoice, Other: "  "}, domain.ErrOtherEmpty},
		{"required, other left empty", required, domain.AnswerValue{Choice: domain.OtherChoice}, domain.ErrOtherEmpty},
		{"required, skipped", required, domain.AnswerValue{}, domain.ErrRequiredAnswer},
		{"single, text beside an option", single, domain.AnswerValue{Choice: "Email", Other: "and more"}, domain.ErrNotAnOption},
		{"single, text and no choice", single, domain.AnswerValue{Other: "Fax"}, domain.ErrNotAnOption},
		{"single, other too long", single, domain.AnswerValue{Choice: domain.OtherChoice, Other: long}, domain.ErrAnswerTooLong},
		{"multiple, options and other", multiple, domain.AnswerValue{Choices: []string{"Email", domain.OtherChoice}, Other: "Fax"}, nil},
		{"multiple, other alone", multiple, domain.AnswerValue{Choices: []string{domain.OtherChoice}, Other: "Fax"}, nil},
		{"multiple, other left empty", multiple, domain.AnswerValue{Choices: []string{"Email", domain.OtherChoice}}, domain.ErrOtherEmpty},
		{"multiple, text without other", multiple, domain.AnswerValue{Choices: []string{"Email"}, Other: "Fax"}, domain.ErrNotAnOption},
		{"not offered, other chosen", without, domain.AnswerValue{Choice: domain.OtherChoice, Other: "Fax"}, domain.ErrNotAnOption},
		{"not offered, marker alone", without, domain.AnswerValue{Choice: domain.OtherChoice}, domain.ErrNotAnOption},
		{"not offered, multiple", withoutMultiple, domain.AnswerValue{Choices: []string{domain.OtherChoice}, Other: "Fax"}, domain.ErrNotAnOption},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := domain.ValidateAnswer(tc.q, tc.v); !errors.Is(err, tc.want) {
				t.Errorf("ValidateAnswer(%+v) = %v, want %v", tc.v, err, tc.want)
			}
		})
	}
}

func TestAnswerValue_OtherDisplayAndRoundTrip(t *testing.T) {
	t.Parallel()
	single := domain.AnswerValue{Choice: domain.OtherChoice, Other: "Carrier pigeon"}
	if got := single.Display(); got != "Other: Carrier pigeon" {
		t.Errorf("Display() = %q", got)
	}
	multiple := domain.AnswerValue{Choices: []string{"Email", domain.OtherChoice}, Other: "Fax"}
	if got := multiple.Display(); got != "Email, Other: Fax" {
		t.Errorf("Display() = %q", got)
	}
	spanish := multiple.DisplayWith(func(written string) string { return "Otra: " + written })
	if spanish != "Email, Otra: Fax" {
		t.Errorf("DisplayWith() = %q", spanish)
	}

	raw, err := single.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"choice":"`+domain.OtherChoice+`","other":"Carrier pigeon"}` {
		t.Errorf("stored as %s", raw)
	}
	back, err := domain.ParseAnswerValue(raw)
	if err != nil {
		t.Fatal(err)
	}
	if back.Choice != single.Choice || back.Other != single.Other {
		t.Errorf("round trip = %+v, want %+v", back, single)
	}

	// A question published before Other existed is read back without it.
	q := domain.Question{Type: domain.SingleChoice, Text: "Q", Options: []string{"a", "b"}}
	draft := domain.Draft{Questions: []domain.Question{q}}
	encoded, err := draft.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "allow_other") {
		t.Errorf("a question without Other carries the flag: %s", encoded)
	}
}
