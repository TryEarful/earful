package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/domain"
)

// Results is a survey's answers, folded by Question Identity across every
// version (ADR-0001). Nothing is copied or migrated: the fold happens
// here, at read time, from responses that stay pinned to the version
// their respondent was actually shown.
type Results struct {
	Responses []ResponseRow
	Questions []QuestionResults
}

// ResponseRow is one submission: the unit a CSV row and a table row are
// built from.
type ResponseRow struct {
	ID            uuid.UUID
	VersionNumber int
	SubmittedAt   time.Time
	DurationSecs  *int
	// ParticipantEmail is set for invited surveys only, and is NULL
	// forever for anonymous ones (ADR-0003).
	ParticipantEmail *string
	// Answers is keyed by Question Identity.
	Answers map[string]domain.AnswerValue
}

// QuestionResults is one question's history and its answers.
type QuestionResults struct {
	IdentityID string
	Type       domain.QuestionType
	// Text is the newest wording; Wordings lists each distinct wording
	// with the version it appeared in, so an edit is visible rather than
	// hidden (story 50).
	Text     string
	Wordings []Wording
	Options  []string
	ScaleMin int
	ScaleMax int
	// FirstVersion/LastVersion bound the question's life: a question
	// added later, or deleted, shows a shorter span than the survey.
	FirstVersion int
	LastVersion  int
	// Answers are in submission order, each carrying the version it was
	// given under.
	Answers []AnswerRow
}

// Wording is one version's phrasing of a question.
type Wording struct {
	VersionNumber int
	Text          string
}

// AnswerRow is one answer to one question.
type AnswerRow struct {
	// ID is the answer's own id, which a cached translation attaches to
	// (M11-T2).
	ID               uuid.UUID
	ResponseID       uuid.UUID
	VersionNumber    int
	SubmittedAt      time.Time
	Value            domain.AnswerValue
	ParticipantEmail *string
}

// SurveyResults assembles everything the results page, the CSV export and
// the insight prompts read from. It is one pass over the survey's answers
// — fine at the scale this product is for, and the place to add paging if
// a survey ever outgrows it.
func (s *Surveys) SurveyResults(ctx context.Context, surveyID uuid.UUID) (Results, error) {
	questionRows, err := s.q.ListQuestionsAcrossVersions(ctx, surveyID)
	if err != nil {
		return Results{}, fmt.Errorf("store: list questions across versions: %w", err)
	}
	answerRows, err := s.q.ListAnswersForSurvey(ctx, surveyID)
	if err != nil {
		return Results{}, fmt.Errorf("store: list answers: %w", err)
	}
	responseRows, err := s.q.ListResponsesForSurvey(ctx, surveyID)
	if err != nil {
		return Results{}, fmt.Errorf("store: list responses: %w", err)
	}

	byIdentity := map[string]*QuestionResults{}
	var order []string
	for _, row := range questionRows {
		identity := row.QuestionIdentityID.String()
		question, seen := byIdentity[identity]
		if !seen {
			question = &QuestionResults{
				IdentityID:   identity,
				FirstVersion: int(row.VersionNumber),
			}
			byIdentity[identity] = question
			order = append(order, identity)
		}
		var options []string
		if len(row.Options) > 0 {
			if err := json.Unmarshal(row.Options, &options); err != nil {
				return Results{}, fmt.Errorf("store: decode options: %w", err)
			}
		}
		// Later versions win for the current shape; the earlier wordings
		// stay in Wordings.
		question.Type = domain.QuestionType(row.Type)
		question.Text = row.Text
		question.Options = options
		question.LastVersion = int(row.VersionNumber)
		if row.ScaleMin != nil {
			question.ScaleMin = int(*row.ScaleMin)
		}
		if row.ScaleMax != nil {
			question.ScaleMax = int(*row.ScaleMax)
		}
		if len(question.Wordings) == 0 || question.Wordings[len(question.Wordings)-1].Text != row.Text {
			question.Wordings = append(question.Wordings, Wording{
				VersionNumber: int(row.VersionNumber), Text: row.Text,
			})
		}
	}

	answersByResponse := map[uuid.UUID]map[string]domain.AnswerValue{}
	for _, row := range answerRows {
		value, err := domain.ParseAnswerValue(row.Value)
		if err != nil {
			return Results{}, fmt.Errorf("store: decode answer: %w", err)
		}
		identity := row.QuestionIdentityID.String()
		if question, ok := byIdentity[identity]; ok {
			question.Answers = append(question.Answers, AnswerRow{
				ID:               row.AnswerID,
				ResponseID:       row.ResponseID,
				VersionNumber:    int(row.VersionNumber),
				SubmittedAt:      row.SubmittedAt,
				Value:            value,
				ParticipantEmail: row.ParticipantEmail,
			})
		}
		if answersByResponse[row.ResponseID] == nil {
			answersByResponse[row.ResponseID] = map[string]domain.AnswerValue{}
		}
		answersByResponse[row.ResponseID][identity] = value
	}

	results := Results{Questions: make([]QuestionResults, 0, len(order))}
	for _, identity := range order {
		results.Questions = append(results.Questions, *byIdentity[identity])
	}
	for _, row := range responseRows {
		answers := answersByResponse[row.ID]
		if answers == nil {
			answers = map[string]domain.AnswerValue{}
		}
		results.Responses = append(results.Responses, ResponseRow{
			ID:               row.ID,
			VersionNumber:    int(row.VersionNumber),
			SubmittedAt:      row.SubmittedAt,
			DurationSecs:     intPtr(row.DurationSecs),
			ParticipantEmail: row.ParticipantEmail,
			Answers:          answers,
		})
	}
	return results, nil
}

func intPtr(v *int32) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

// AsQuestion rebuilds the domain question, so scales and options behave
// exactly as they do for a respondent (including the pre-00009 fallback).
func (q QuestionResults) AsQuestion() domain.Question {
	return domain.Question{
		IdentityID: q.IdentityID,
		Type:       q.Type,
		Text:       q.Text,
		Options:    q.Options,
		ScaleMin:   q.ScaleMin,
		ScaleMax:   q.ScaleMax,
	}
}

// Reworded reports whether the question was phrased differently in
// different versions — the case the results page has to label rather than
// smooth over.
func (q QuestionResults) Reworded() bool { return len(q.Wordings) > 1 }
