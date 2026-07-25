package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/domain"
	"github.com/TryEarful/earful/internal/store/db"
)

// Reading frozen Localizations back (M11-T1) and caching answer
// translations (M11-T2).

// VersionLanguages lists the languages a published version was frozen
// with — the only languages a respondent can be served, because adding
// one later means publishing a new version.
func (s *Surveys) VersionLanguages(ctx context.Context, versionID uuid.UUID) ([]string, error) {
	langs, err := s.q.ListVersionLanguages(ctx, versionID)
	if err != nil {
		return nil, fmt.Errorf("store: list version languages: %w", err)
	}
	return langs, nil
}

// LocalizedQuestions returns a version's questions with lang's frozen
// translations applied. A question with no translation keeps its
// original wording rather than disappearing: a partly translated survey
// is still answerable.
func (s *Surveys) LocalizedQuestions(ctx context.Context, versionID uuid.UUID, lang string) ([]domain.Question, error) {
	questions, err := s.QuestionsForVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	lang = domain.NormalizeLang(lang)
	if lang == "" {
		return questions, nil
	}
	rows, err := s.q.ListLocalizationsForVersion(ctx, versionID)
	if err != nil {
		return nil, fmt.Errorf("store: list localizations: %w", err)
	}

	byIdentity := map[string]db.ListLocalizationsForVersionRow{}
	for _, row := range rows {
		if domain.NormalizeLang(row.Lang) == lang {
			byIdentity[row.QuestionIdentityID.String()] = row
		}
	}
	for i, question := range questions {
		row, ok := byIdentity[question.IdentityID]
		if !ok {
			continue
		}
		questions[i].Text = row.Text
		var options []string
		if len(row.Options) > 0 {
			if err := json.Unmarshal(row.Options, &options); err != nil {
				return nil, fmt.Errorf("store: decode localized options: %w", err)
			}
		}
		if len(options) == len(question.Options) {
			// Only swap a complete option set: a partial one would change
			// what an answer means.
			questions[i].Options = options
		}
	}
	return questions, nil
}

// AnswerTranslation is one cached machine translation of one answer.
type AnswerTranslation struct {
	AnswerID uuid.UUID
	Lang     string
	Text     string
	Model    string
}

// SaveAnswerTranslation caches a translation. The original answer is
// never touched — that is the whole point (story 26).
func (s *Surveys) SaveAnswerTranslation(ctx context.Context, t AnswerTranslation, now time.Time) error {
	err := s.q.UpsertAnswerTranslation(ctx, db.UpsertAnswerTranslationParams{
		AnswerID:  t.AnswerID,
		Lang:      domain.NormalizeLang(t.Lang),
		Text:      t.Text,
		Model:     t.Model,
		CreatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("store: save answer translation: %w", err)
	}
	return nil
}

// AnswerTranslations returns a survey's cached translations in one
// language, keyed by answer.
func (s *Surveys) AnswerTranslations(ctx context.Context, surveyID uuid.UUID, lang string) (map[uuid.UUID]AnswerTranslation, error) {
	rows, err := s.q.ListAnswerTranslations(ctx, db.ListAnswerTranslationsParams{
		SurveyID: surveyID, Lang: domain.NormalizeLang(lang),
	})
	if err != nil {
		return nil, fmt.Errorf("store: list answer translations: %w", err)
	}
	out := make(map[uuid.UUID]AnswerTranslation, len(rows))
	for _, row := range rows {
		out[row.AnswerID] = AnswerTranslation{
			AnswerID: row.AnswerID, Lang: row.Lang, Text: row.Text, Model: row.Model,
		}
	}
	return out, nil
}
