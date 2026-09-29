// Package starter writes the Starter Survey (story 86, ADR-0015): the
// published survey a Workspace holds from the moment it is created.
//
// The survey is an ordinary one once written. What is decided here is
// only what it says and what it asks with; the wording is in web/text,
// under `starter`, so that it is rewritten where every other sentence
// is.
package starter

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/store/db"
	"github.com/TryEarful/earful/internal/uitext"
)

// Translated is the language the survey is published in besides the
// source. It is fixed rather than taken from uitext.Served: a language
// added to the interface has not thereby been read as a survey, and a
// Localization nobody has read is what story 23 keeps from being
// published.
const Translated = "es"

// questions are the survey, in the order they are asked. The first can
// be spoken, because dictation is what a respondent should meet first.
// shape is everything about a question but its wording and its identity.
var questions = []struct {
	shape   domain.Question
	text    uitext.ID
	options []uitext.ID
}{
	{
		shape: domain.Question{Type: domain.LongText},
		text:  "starter.heard.text",
	},
	{
		shape: domain.Question{Type: domain.RatingScale, Required: true, ScaleMin: 1, ScaleMax: 5},
		text:  "starter.satisfaction.text",
	},
	{
		shape: domain.Question{Type: domain.SingleChoice},
		text:  "starter.frequency.text",
		options: []uitext.ID{
			"starter.frequency.weekly",
			"starter.frequency.monthly",
			"starter.frequency.yearly",
			"starter.frequency.once",
		},
	},
	{
		shape: domain.Question{Type: domain.LongText},
		text:  "starter.missing.text",
	},
	{
		shape: domain.Question{Type: domain.NPS, Required: true},
		text:  "starter.recommend.text",
	},
}

// Survey returns the Starter Survey's title and its draft, worded from c:
// the questions in the source language, and a reviewed Localization in
// Translated. The language of whoever is asking plays no part.
//
// Every call mints new Question Identities. An identity belongs to one
// survey, and a second survey published with the identities of the first
// would have its questions attached to the first survey's.
func Survey(c *uitext.Catalog) (string, domain.Draft, error) {
	if !c.Serves(Translated) {
		return "", domain.Draft{}, fmt.Errorf("starter: the interface text has no %q to publish the survey in", Translated)
	}
	source, translated := c.Localizer(uitext.Source), c.Localizer(Translated)

	var draft domain.Draft
	if err := draft.AddLanguage(Translated); err != nil {
		return "", domain.Draft{}, fmt.Errorf("starter: %w", err)
	}
	for _, q := range questions {
		question := q.shape
		question.IdentityID = uuid.NewString()
		question.Text = source.T(q.text)
		var options []string
		for _, option := range q.options {
			question.Options = append(question.Options, source.T(option))
			options = append(options, translated.T(option))
		}
		if err := draft.Add(question); err != nil {
			return "", domain.Draft{}, fmt.Errorf("starter: %s: %w", q.text, err)
		}
		// Reviewed: the translation was written and read by a person, in
		// web/text, which is what the flag records.
		if err := draft.SetTranslation(Translated, question.IdentityID, translated.T(q.text), options, true); err != nil {
			return "", domain.Draft{}, fmt.Errorf("starter: %s: %w", q.text, err)
		}
	}
	return source.T("starter.title"), draft, nil
}

// Seeder returns what gives a new Workspace its Starter Survey, inside
// the transaction that creates the Workspace.
func Seeder(c *uitext.Catalog) auth.WorkspaceSeeder {
	return func(ctx context.Context, q *db.Queries, workspaceID, userID uuid.UUID, now time.Time) error {
		title, draft, err := Survey(c)
		if err != nil {
			return err
		}
		_, err = store.SeedStarterSurvey(ctx, q, workspaceID, userID, title, draft, now)
		return err
	}
}
