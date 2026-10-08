package starter_test

import (
	"testing"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/starter"
	"github.com/TryEarful/earful/internal/uitext"
	"github.com/TryEarful/earful/web/text"
)

// catalog is the interface text as the binary carries it, made strict: a
// question whose message is missing fails the test rather than being
// published as its own name.
func catalog(t *testing.T) *uitext.Catalog {
	t.Helper()
	c, err := uitext.Load(text.FS, uitext.Options{Strict: true})
	if err != nil {
		t.Fatalf("interface text: %v", err)
	}
	return c
}

// TestSurvey_CanBePublishedAsItIs: the survey is published without a
// person looking at it, inside the transaction that creates a workspace,
// so a fault in it is a fault in signing up. It has to pass, as written,
// every check the editor would have put it through.
func TestSurvey_CanBePublishedAsItIs(t *testing.T) {
	t.Parallel()
	title, draft, err := starter.Survey(catalog(t))
	if err != nil {
		t.Fatalf("write the survey: %v", err)
	}

	if err := domain.ValidateTitle(title); err != nil {
		t.Errorf("title %q: %v", title, err)
	}
	if err := draft.ValidateForPublish(); err != nil {
		t.Errorf("the questions cannot be published: %v", err)
	}
	if err := draft.ReadyToPublish(domain.WorkspaceStyle{}); err != nil {
		t.Errorf("the translation cannot be published: %v", err)
	}
	if got := len(draft.Questions); got != 5 {
		t.Errorf("the survey asks %d questions, want 5", got)
	}
}

// TestSurvey_OpensOnAQuestionThatCanBeSpoken: dictation is what a
// respondent should meet first, and a respondent meets one question at a
// time.
func TestSurvey_OpensOnAQuestionThatCanBeSpoken(t *testing.T) {
	t.Parallel()
	_, draft, err := starter.Survey(catalog(t))
	if err != nil {
		t.Fatalf("write the survey: %v", err)
	}
	if first := draft.Questions[0]; !first.Type.AcceptsVoice() {
		t.Errorf("the first question is a %s, which cannot be answered by voice", first.Type)
	}
}

// TestSurvey_IsTranslatedInFull: a question left in English in the
// translation would be published as Spanish, reviewed, and read by a
// respondent who asked for Spanish.
func TestSurvey_IsTranslatedInFull(t *testing.T) {
	t.Parallel()
	_, draft, err := starter.Survey(catalog(t))
	if err != nil {
		t.Fatalf("write the survey: %v", err)
	}

	if got := draft.Languages(); len(got) != 1 || got[0] != starter.Translated {
		t.Fatalf("the survey's languages are %v, want only %q", got, starter.Translated)
	}
	translation := draft.Localizations[starter.Translated]
	for i, question := range draft.Questions {
		translated, ok := translation.Questions[question.IdentityID]
		if !ok {
			t.Errorf("question %d has no translation", i+1)
			continue
		}
		if translated.Text == question.Text {
			t.Errorf("question %d reads the same in both languages: %q", i+1, question.Text)
		}
		if len(translated.Options) != len(question.Options) {
			t.Errorf("question %d has %d options and %d translated", i+1, len(question.Options), len(translated.Options))
			continue
		}
		for j := range question.Options {
			if translated.Options[j] == question.Options[j] {
				t.Errorf("question %d, option %d reads the same in both languages: %q", i+1, j+1, question.Options[j])
			}
		}
	}
}

// TestSurvey_IsWrittenAnewEachTime: a Question Identity belongs to one
// survey. Two surveys sharing one would have the second's questions
// recorded under the first.
func TestSurvey_IsWrittenAnewEachTime(t *testing.T) {
	t.Parallel()
	c := catalog(t)
	_, first, err := starter.Survey(c)
	if err != nil {
		t.Fatalf("write the survey: %v", err)
	}
	_, second, err := starter.Survey(c)
	if err != nil {
		t.Fatalf("write the survey again: %v", err)
	}

	seen := map[string]bool{}
	for _, question := range first.Questions {
		seen[question.IdentityID] = true
	}
	for i, question := range second.Questions {
		if seen[question.IdentityID] {
			t.Errorf("question %d has the identity it had in an earlier survey", i+1)
		}
	}
}

// TestSurvey_NeedsItsSecondLanguage: an instance serving English alone
// has nothing to publish the translation from, and says so rather than
// publishing English twice.
func TestSurvey_NeedsItsSecondLanguage(t *testing.T) {
	t.Parallel()
	c, err := uitext.Load(text.FS, uitext.Options{Strict: true, Languages: []string{uitext.Source}})
	if err != nil {
		t.Fatalf("interface text: %v", err)
	}
	if _, _, err := starter.Survey(c); err == nil {
		t.Error("the survey was written without the language it is translated into")
	}
}
