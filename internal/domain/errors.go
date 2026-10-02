package domain

import (
	"fmt"
	"strconv"
	"strings"
)

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
	LimitBounds       LimitKind = "bounds"
	LimitQuestions    LimitKind = "questions"
	LimitLanguages    LimitKind = "languages"
	// The thank you page's message and link label.
	LimitThanksMessage LimitKind = "thanks_message"
	LimitThanksLabel   LimitKind = "thanks_label"
	// A style's header and footer (ADR-0018).
	LimitStyleName    LimitKind = "style_name"
	LimitStyleTagline LimitKind = "style_tagline"
	LimitStyleFooter  LimitKind = "style_footer"
	LimitStyleLinks   LimitKind = "style_links"
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
	case LimitBounds:
		return fmt.Sprintf("the lowest answer must be below the highest, and both between %s and %s",
			grouped(-e.Limit), grouped(e.Limit))
	case LimitQuestions:
		return fmt.Sprintf("a survey can hold at most %d questions", e.Limit)
	case LimitLanguages:
		return fmt.Sprintf("a survey can carry at most %d languages", e.Limit)
	case LimitThanksMessage:
		return fmt.Sprintf("keep the thank you message under %d characters", e.Limit)
	case LimitThanksLabel:
		return fmt.Sprintf("keep the link label under %d characters", e.Limit)
	case LimitStyleName:
		return fmt.Sprintf("keep the name under %d characters", e.Limit)
	case LimitStyleTagline:
		return fmt.Sprintf("keep the tagline under %d characters", e.Limit)
	case LimitStyleFooter:
		return fmt.Sprintf("keep the footer text under %d characters", e.Limit)
	case LimitStyleLinks:
		return fmt.Sprintf("add at most %d links", e.Limit)
	}
	return fmt.Sprintf("over the limit of %d", e.Limit)
}

// RangeError reports a number answer that is not a whole number within
// the question's limits. It carries the limits, so the respondent can be
// told in their own language what to type.
type RangeError struct {
	Min, Max int
}

func (e RangeError) Error() string {
	return fmt.Sprintf("enter a whole number from %s to %s", grouped(e.Min), grouped(e.Max))
}

// grouped writes n with its digits in threes, as the English a person
// is shown writes it.
func grouped(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}
	return sign + digits
}

// QuestionError says which question of a draft a problem belongs to.
type QuestionError struct {
	// Position counts from one, as a person would.
	Position int
	Err      error
}

func (e QuestionError) Error() string { return fmt.Sprintf("question %d: %v", e.Position, e.Err) }

func (e QuestionError) Unwrap() error { return e.Err }
