package domain

import "fmt"

// A validation error is read twice: by the code that decides what to do
// about it, and by the person who has to put it right. The sentinels
// above their validators serve the first. The two types here are for
// the errors that carry something besides their identity, a limit or a
// position, which a reader in another language needs as a number rather
// than as part of an English sentence.
//
// Error() is English throughout, for logs and for tests. The wording a
// person is shown is chosen where the person's language is known, which
// is not here.

// LimitKind says what there is too much of.
type LimitKind string

const (
	LimitAnswerText   LimitKind = "answer"
	LimitQuestionText LimitKind = "question"
	LimitTitle        LimitKind = "title"
	LimitScale        LimitKind = "scale"
	LimitQuestions    LimitKind = "questions"
	LimitLanguages    LimitKind = "languages"
)

// LimitError reports something longer, higher or more numerous than it
// may be. It is comparable, so a LimitError can be a sentinel and
// errors.Is finds it.
type LimitError struct {
	Kind  LimitKind
	Limit int
}

func (e LimitError) Error() string {
	switch e.Kind {
	case LimitAnswerText:
		return fmt.Sprintf("please keep the answer under %d characters", e.Limit)
	case LimitQuestionText:
		return fmt.Sprintf("keep the question under %d characters", e.Limit)
	case LimitTitle:
		return fmt.Sprintf("keep the title under %d characters", e.Limit)
	case LimitScale:
		return fmt.Sprintf("the scale must start at 0 or 1 and end no higher than %d", e.Limit)
	case LimitQuestions:
		return fmt.Sprintf("a survey can hold at most %d questions", e.Limit)
	case LimitLanguages:
		return fmt.Sprintf("a survey can carry at most %d languages", e.Limit)
	}
	return fmt.Sprintf("over the limit of %d", e.Limit)
}

// QuestionError says which question of a draft a problem belongs to.
type QuestionError struct {
	// Position counts from one, as a person would.
	Position int
	Err      error
}

func (e QuestionError) Error() string { return fmt.Sprintf("question %d: %v", e.Position, e.Err) }

func (e QuestionError) Unwrap() error { return e.Err }
