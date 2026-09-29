package templates

import (
	"context"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/uitext"
)

// What is stored is a value: a survey is "Open", a question is a
// "single_choice". What is shown is a word, in the reader's language.
// These are the messages for the values, written out one by one so that
// each can be found where it is used. A value with no message is shown
// as it is stored, which is how it was shown before it had one.

// StatusName is the message for a Survey Status.
func StatusName(s domain.Status) uitext.ID {
	switch s {
	case domain.StatusDraft:
		return "survey.status.draft"
	case domain.StatusOpen:
		return "survey.status.open"
	case domain.StatusClosed:
		return "survey.status.closed"
	}
	return ""
}

// QuestionTypeName is the message for a kind of question.
func QuestionTypeName(kind domain.QuestionType) uitext.ID {
	switch kind {
	case domain.LongText:
		return "question.type.long_text.name"
	case domain.ShortText:
		return "question.type.short_text.name"
	case domain.SingleChoice:
		return "question.type.single_choice.name"
	case domain.MultipleChoice:
		return "question.type.multiple_choice.name"
	case domain.RatingScale:
		return "question.type.rating_scale.name"
	case domain.NPS:
		return "question.type.nps.name"
	case domain.YesNo:
		return "question.type.yes_no.name"
	case domain.Dropdown:
		return "question.type.dropdown.name"
	}
	return ""
}

// QuestionTypeHint is the message that says when to reach for a kind of
// question.
func QuestionTypeHint(kind domain.QuestionType) uitext.ID {
	switch kind {
	case domain.LongText:
		return "question.type.long_text.hint"
	case domain.ShortText:
		return "question.type.short_text.hint"
	case domain.SingleChoice:
		return "question.type.single_choice.hint"
	case domain.MultipleChoice:
		return "question.type.multiple_choice.hint"
	case domain.RatingScale:
		return "question.type.rating_scale.hint"
	case domain.NPS:
		return "question.type.nps.hint"
	case domain.YesNo:
		return "question.type.yes_no.hint"
	case domain.Dropdown:
		return "question.type.dropdown.hint"
	}
	return ""
}

// Named renders the message for a value, or the value where it has none.
func Named(l uitext.Localizer, id uitext.ID, stored string) string {
	if id == "" {
		return stored
	}
	return l.T(id)
}

func statusName(ctx context.Context, s domain.Status) string {
	return Named(uitext.From(ctx), StatusName(s), string(s))
}

func typeName(ctx context.Context, kind domain.QuestionType) string {
	return Named(uitext.From(ctx), QuestionTypeName(kind), string(kind))
}

func typeHint(ctx context.Context, kind domain.QuestionType) string {
	return Named(uitext.From(ctx), QuestionTypeHint(kind), "")
}
