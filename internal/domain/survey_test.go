package domain_test

import (
	"testing"
	"time"

	"github.com/TryEarful/earful/internal/domain"
)

func TestStatusAt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name  string
		state domain.SurveyState
		want  domain.Status
	}{
		{"never published", domain.SurveyState{}, domain.StatusDraft},
		{"never published with close date", domain.SurveyState{CloseAt: &past}, domain.StatusDraft},
		{"published", domain.SurveyState{HasPublishedVersion: true}, domain.StatusOpen},
		{"published, close date ahead", domain.SurveyState{HasPublishedVersion: true, CloseAt: &future}, domain.StatusOpen},
		{"published, close date passed", domain.SurveyState{HasPublishedVersion: true, CloseAt: &past}, domain.StatusClosed},
		{"manually closed", domain.SurveyState{HasPublishedVersion: true, ClosedAt: &past}, domain.StatusClosed},
		{"manually closed beats a future date", domain.SurveyState{HasPublishedVersion: true, ClosedAt: &past, CloseAt: &future}, domain.StatusClosed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.state.StatusAt(now); got != tc.want {
				t.Errorf("StatusAt = %q, want %q", got, tc.want)
			}
			wantAccepts := tc.want == domain.StatusOpen
			if got := tc.state.AcceptsResponses(now); got != wantAccepts {
				t.Errorf("AcceptsResponses = %v, want %v", got, wantAccepts)
			}
		})
	}
}

// TestStatusAt_ClosesExactlyOnTheBoundary: the close instant itself is
// already closed, so a survey never accepts a response at or after it.
func TestStatusAt_ClosesExactlyOnTheBoundary(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	state := domain.SurveyState{HasPublishedVersion: true, CloseAt: &at}

	if got := state.StatusAt(at.Add(-time.Nanosecond)); got != domain.StatusOpen {
		t.Errorf("just before close: %q, want Open", got)
	}
	if got := state.StatusAt(at); got != domain.StatusClosed {
		t.Errorf("at the close instant: %q, want Closed", got)
	}
}

func TestDraft_AddReplaceRemoveMove(t *testing.T) {
	t.Parallel()
	var d domain.Draft
	q := func(id, text string) domain.Question {
		return domain.Question{IdentityID: id, Type: domain.ShortText, Text: text}
	}

	for _, spec := range []struct{ id, text string }{{"a", "first"}, {"b", "second"}, {"c", "third"}} {
		if err := d.Add(q(spec.id, spec.text)); err != nil {
			t.Fatalf("Add(%s): %v", spec.id, err)
		}
	}
	if got := order(d); got != "a,b,c" {
		t.Fatalf("initial order = %s", got)
	}

	if err := d.Move("c", -1); err != nil {
		t.Fatalf("Move up: %v", err)
	}
	if got := order(d); got != "a,c,b" {
		t.Errorf("after moving c up = %s, want a,c,b", got)
	}

	if err := d.Move("a", -1); err != nil {
		t.Fatalf("Move first up: %v", err)
	}
	if got := order(d); got != "a,c,b" {
		t.Errorf("moving the first question up should be a no-op, got %s", got)
	}

	if err := d.Move("b", 5); err != nil {
		t.Fatalf("Move far down: %v", err)
	}
	if got := order(d); got != "a,c,b" {
		t.Errorf("moving the last question down should clamp, got %s", got)
	}

	if err := d.Replace("c", q("ignored", "third, reworded")); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	i, ok := d.IndexOf("c")
	if !ok {
		t.Fatal("Replace lost the question identity")
	}
	if d.Questions[i].Text != "third, reworded" {
		t.Errorf("Replace did not update the text: %q", d.Questions[i].Text)
	}

	if err := d.Remove("c"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := order(d); got != "a,b" {
		t.Errorf("after Remove = %s, want a,b", got)
	}

	for _, err := range []error{d.Replace("gone", q("gone", "x")), d.Remove("gone"), d.Move("gone", 1)} {
		if err == nil {
			t.Error("operating on an unknown question should fail")
		}
	}
}

func TestDraft_RoundTrip(t *testing.T) {
	t.Parallel()
	original := domain.Draft{Questions: []domain.Question{
		{IdentityID: "a", Type: domain.SingleChoice, Text: "Pick", Options: []string{"x", "y"}, Required: true},
		{IdentityID: "b", Type: domain.RatingScale, Text: "Rate", ScaleMin: 1, ScaleMax: 5},
	}}

	encoded, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	parsed, err := domain.ParseDraft(encoded)
	if err != nil {
		t.Fatalf("ParseDraft: %v", err)
	}
	if len(parsed.Questions) != 2 {
		t.Fatalf("round trip lost questions: %+v", parsed)
	}
	if parsed.Questions[0].Options[1] != "y" || parsed.Questions[1].ScaleMax != 5 {
		t.Errorf("round trip corrupted a question: %+v", parsed.Questions)
	}

	if empty, err := domain.ParseDraft(nil); err != nil || len(empty.Questions) != 0 {
		t.Errorf("empty document should parse to an empty draft, got %+v (%v)", empty, err)
	}
}

func TestQuestion_Validate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		q       domain.Question
		wantErr bool
	}{
		{"valid long text", domain.Question{Type: domain.LongText, Text: "Tell me"}, false},
		{"blank text", domain.Question{Type: domain.LongText, Text: "  "}, true},
		{"unknown type", domain.Question{Type: "seance", Text: "Ask the void"}, true},
		{"choice needs options", domain.Question{Type: domain.SingleChoice, Text: "Pick"}, true},
		{"choice with two options", domain.Question{Type: domain.SingleChoice, Text: "Pick", Options: []string{"a", "b"}}, false},
		{"blank option", domain.Question{Type: domain.Dropdown, Text: "Pick", Options: []string{"a", "  "}}, true},
		{"case-insensitive duplicate", domain.Question{Type: domain.Dropdown, Text: "Pick", Options: []string{"Yes", "yes"}}, true},
		{"valid scale", domain.Question{Type: domain.RatingScale, Text: "Rate", ScaleMin: 1, ScaleMax: 5}, false},
		{"scale too wide", domain.Question{Type: domain.RatingScale, Text: "Rate", ScaleMin: 1, ScaleMax: 50}, true},
		{"inverted scale", domain.Question{Type: domain.RatingScale, Text: "Rate", ScaleMin: 1, ScaleMax: 1}, true},
		{"nps needs no options or scale", domain.Question{Type: domain.NPS, Text: "Recommend?"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.q.Validate()
			if tc.wantErr && err == nil {
				t.Error("expected a validation error, got none")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestDraft_ValidateForPublish(t *testing.T) {
	t.Parallel()
	if err := (domain.Draft{}).ValidateForPublish(); err == nil {
		t.Error("an empty draft must not be publishable")
	}

	// A draft holding a question that would fail today's rules cannot be
	// published, even though it was accepted when saved.
	stale := domain.Draft{Questions: []domain.Question{
		{IdentityID: "a", Type: domain.SingleChoice, Text: "Pick", Options: []string{"only one"}},
	}}
	if err := stale.ValidateForPublish(); err == nil {
		t.Error("publish must re-validate every question")
	}

	good := domain.Draft{Questions: []domain.Question{
		{IdentityID: "a", Type: domain.YesNo, Text: "Ready?"},
	}}
	if err := good.ValidateForPublish(); err != nil {
		t.Errorf("valid draft rejected: %v", err)
	}
}

func TestQuestionType_Traits(t *testing.T) {
	t.Parallel()
	if !domain.LongText.AcceptsVoice() || !domain.ShortText.AcceptsVoice() {
		t.Error("text questions must accept voice answers (ADR-0004)")
	}
	if domain.SingleChoice.AcceptsVoice() || domain.NPS.AcceptsVoice() {
		t.Error("non-text questions must not offer voice")
	}
	// NPS spans 0-10 by definition, whatever bounds a question carries.
	nps := domain.Question{Type: domain.NPS, Text: "Recommend?", ScaleMin: 3, ScaleMax: 4}
	if min, max := nps.Scale(); min != 0 || max != 10 {
		t.Errorf("NPS scale = %d-%d, want 0-10", min, max)
	}
	if got := len(nps.ScalePoints()); got != 11 {
		t.Errorf("NPS has %d points, want 11", got)
	}
	rating := domain.Question{Type: domain.RatingScale, Text: "Rate", ScaleMin: 1, ScaleMax: 5}
	if got := len(rating.ScalePoints()); got != 5 {
		t.Errorf("1-5 rating has %d points, want 5", got)
	}
	// Versions published before migration 00009 stored no bounds at all.
	// Such a question must read back as the editor's default scale, never
	// as the degenerate 0-0 that used to render a single radio.
	legacy := domain.Question{Type: domain.RatingScale, Text: "Rate"}
	if min, max := legacy.Scale(); min != domain.DefaultRatingScaleMin || max != domain.DefaultRatingScaleMax {
		t.Errorf("boundless rating scale = %d-%d, want %d-%d",
			min, max, domain.DefaultRatingScaleMin, domain.DefaultRatingScaleMax)
	}
	for _, tc := range []struct {
		t     domain.QuestionType
		needs bool
	}{
		{domain.SingleChoice, true}, {domain.MultipleChoice, true}, {domain.Dropdown, true},
		{domain.LongText, false}, {domain.YesNo, false}, {domain.NPS, false}, {domain.RatingScale, false},
	} {
		if got := tc.t.NeedsOptions(); got != tc.needs {
			t.Errorf("%s.NeedsOptions() = %v, want %v", tc.t, got, tc.needs)
		}
	}
	if !domain.RatingScale.NeedsScale() || domain.NPS.NeedsScale() {
		t.Error("only rating scales carry author-set bounds; NPS is fixed at 0-10")
	}
	for _, qt := range domain.QuestionTypes {
		if qt.Label() == "" || qt.Hint() == "" {
			t.Errorf("%s is missing its editor label or hint", qt)
		}
	}
}

func TestValidateTitle(t *testing.T) {
	t.Parallel()
	if err := domain.ValidateTitle("  "); err == nil {
		t.Error("a blank title must be rejected")
	}
	if err := domain.ValidateTitle("Quarterly retro"); err != nil {
		t.Errorf("valid title rejected: %v", err)
	}
	long := make([]rune, 201)
	for i := range long {
		long[i] = 'x'
	}
	if err := domain.ValidateTitle(string(long)); err == nil {
		t.Error("an over-long title must be rejected")
	}
}

func order(d domain.Draft) string {
	ids := make([]string, 0, len(d.Questions))
	for _, q := range d.Questions {
		ids = append(ids, q.IdentityID)
	}
	return joinComma(ids)
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}
