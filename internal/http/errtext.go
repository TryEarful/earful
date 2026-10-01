package http

import (
	"errors"
	"net/http"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
)

// A validation error is something a person did, and the person is told
// in their own language. The errors themselves are made in packages that
// know nothing of languages, and say what they have to say in English
// for the log; this file is where each one is given its message.
//
// An error that has a message here is a person's to put right, and is
// shown beside the form. One that has none is the service's own
// failure. The two used to be told apart by how the English began.

// plainErrors are the errors that are wholly their identity.
var plainErrors = []struct {
	err error
	id  uitext.ID
}{
	{domain.ErrRequiredAnswer, "answer.error.required"},
	{domain.ErrNotAnOption, "answer.error.option"},
	{domain.ErrOutOfRange, "answer.error.range"},
	{domain.ErrNotADate, "answer.error.date"},
	{domain.ErrEmptyQuestionText, "question.error.empty"},
	{domain.ErrUnknownType, "question.error.kind"},
	{domain.ErrTooFewOptions, "question.error.options"},
	{domain.ErrEmptyOption, "question.error.blank"},
	{domain.ErrDuplicateOption, "question.error.duplicate"},
	{domain.ErrDraftEmpty, "survey.error.empty"},
	{domain.ErrQuestionUnknown, "survey.error.question"},
	{domain.ErrEmptyTitle, "survey.error.untitled"},
	{domain.ErrUnknownLanguage, "language.error.unknown"},
	{domain.ErrLanguageInvalid, "language.error.invalid"},
	{domain.ErrLanguageExists, "language.error.exists"},
	{domain.ErrUnreviewedTrans, "language.error.unreviewed"},
}

// limitError is the message for an error that carries a limit, by what
// the limit is on. ErrAnswerTooLong, ErrBadScale, ErrBadBounds,
// ErrDraftTooLong and ErrTooManyLanguages are errors of this kind.
func limitError(kind domain.LimitKind) (uitext.ID, bool) {
	switch kind {
	case domain.LimitAnswerText:
		return "answer.error.long", true
	case domain.LimitQuestionText:
		return "question.error.long", true
	case domain.LimitTitle:
		return "survey.error.title", true
	case domain.LimitScale:
		return "question.error.scale", true
	case domain.LimitQuestions:
		return "survey.error.long", true
	case domain.LimitBounds:
		return "question.error.bounds", true
	case domain.LimitLanguages:
		return "language.error.limit", true
	}
	return "", false
}

// errorText words err for the person who can put it right, and reports
// whether it is theirs to put right at all.
func errorText(l uitext.Localizer, err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var numbered domain.QuestionError
	if errors.As(err, &numbered) {
		problem, ok := errorText(l, numbered.Err)
		if !ok {
			return "", false
		}
		return l.T("question.error.numbered", uitext.Args{"Position": numbered.Position, "Problem": problem}), true
	}
	var outside domain.RangeError
	if errors.As(err, &outside) {
		return l.T("answer.error.number", uitext.Args{
			"Min": l.Count(int64(outside.Min)), "Max": l.Count(int64(outside.Max)),
		}), true
	}
	var limit domain.LimitError
	if errors.As(err, &limit) {
		if id, ok := limitError(limit.Kind); ok {
			// A number's limits are written out as the two ends, so the
			// minus sign is part of a number, never of the wording.
			return l.T(id, uitext.Args{
				"Limit": limit.Limit,
				"Low":   l.Count(int64(-limit.Limit)),
				"High":  l.Count(int64(limit.Limit)),
			}), true
		}
		return "", false
	}
	if errors.Is(err, store.ErrImportTooLarge) {
		return l.T("editor.refused.import", uitext.Args{"Limit": store.MaxImportBatch}), true
	}
	for _, plain := range plainErrors {
		if errors.Is(err, plain.err) {
			return l.T(plain.id), true
		}
	}
	return "", false
}

// isUserError distinguishes validation problems — which belong inline on
// the form — from infrastructure failures.
func isUserError(err error) bool {
	// The wording is not wanted, only whether there is any; the source
	// language is as good as another to ask in.
	_, ok := errorText(uitext.Embedded().Localizer(), err)
	return ok
}

// sayError words a validation error for the request it is shown to. An
// error with no message of its own is shown as it describes itself,
// which is how every error was shown before any had one.
func sayError(r *http.Request, err error) string {
	if message, ok := errorText(text(r), err); ok {
		return message
	}
	return err.Error()
}
