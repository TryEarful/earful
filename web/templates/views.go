package templates

import "github.com/TryEarful/earful/internal/domain"

// View types carry pre-formatted, presentation-ready data into the
// templates. Formatting decisions (dates, status labels) live in the
// handler layer that builds these, so templates stay free of logic.

// SurveyView is one survey as the creator UI shows it.
type SurveyView struct {
	ID          string
	Title       string
	Status      domain.Status
	IsAnonymous bool
	// CloseAtInput is the yyyy-mm-dd value for <input type="date">, empty
	// when no Close Date is set.
	CloseAtInput string
	// CloseAtLabel is the human rendering, e.g. "3 August 2026".
	CloseAtLabel string
	// ManuallyClosed distinguishes "creator pressed Close" from "the
	// Close Date passed", which the UI words differently.
	ManuallyClosed bool
	LatestVersion  int
	QuestionCount  int
	CreatedAt      string
}

// Published reports whether a version exists yet.
func (s SurveyView) Published() bool { return s.LatestVersion > 0 }

// ShareURL is the public link respondents use (live from M4).
func (s SurveyView) ShareURL() string { return "/s/" + s.ID }

// VersionView is one published version in the version list.
type VersionView struct {
	Number      int
	PublishedAt string
	PublishedBy string
}

// AuditEntry is one line of the derived Audit Log: a draft save or a
// publish, with who and when.
type AuditEntry struct {
	When string
	Who  string
	What string
	// Publish marks version entries so the template can emphasise them.
	Publish bool
}

type SurveyListData struct {
	Surveys []SurveyView
}

// ParticipantView is one row of the participants list.
type ParticipantView struct {
	Email  string
	Status string
}

type SurveyEditorData struct {
	Survey        SurveyView
	Questions     []domain.Question
	Versions      []VersionView
	ResponseCount int
	// AIEnabled shows the "draft with AI" panel. False when no text
	// provider is configured: an absent capability is an absent feature,
	// not a button that fails (Appendix D).
	AIEnabled bool
	// Participants is populated for invited surveys only.
	Participants []ParticipantView
	PendingCount int
	Error        string
	Notice       string
}

type SurveyAuditData struct {
	Survey   SurveyView
	Entries  []AuditEntry
	Versions []VersionView
}

// --- results (M7) --------------------------------------------------------

// SurveyResultsData is the results page. Everything here is already
// formatted: the template counts nothing and rounds nothing.
type SurveyResultsData struct {
	Survey        SurveyView
	ResponseCount int
	Questions     []QuestionResultsView
	Stats         SurveyStatsView
}

// SurveyStatsView is ADR-0009's blessed list and nothing else: how many
// people opened the survey, how many finished, how long it took them,
// where answers stop, and three coarse facts about the audience — each
// suppressed below five observations.
type SurveyStatsView struct {
	Starts          int
	Completions     int
	CompletionRate  string
	AverageDuration string
	LastAnswered    []CountView
	Browsers        []CountView
	Devices         []CountView
	Countries       []CountView
	HasAudience     bool
	SuppressionNote string
}

// QuestionResultsView is one question's results, folded across versions
// by Question Identity (ADR-0001).
type QuestionResultsView struct {
	IdentityID string
	Type       domain.QuestionType
	TypeLabel  string
	// Text is the current wording. Wordings is non-empty only when the
	// question was reworded, in which case each version's phrasing is
	// shown rather than smoothed over (story 50).
	Wordings []WordingView
	Text     string
	Answered int
	// SkippedNote is empty when nobody skipped the question.
	SkippedNote string
	// Distribution is filled for countable types, Texts for text ones.
	Distribution []CountView
	Summary      string
	Texts        []TextAnswerView
}

type WordingView struct {
	Label string
	Text  string
}

// CountView is one bar: a label, its count, and the share as both a
// number (for the bar) and a string (for the reader).
type CountView struct {
	Label   string
	Count   int
	Percent int
	Share   string
}

// TextAnswerView is one written or spoken answer.
type TextAnswerView struct {
	Text         string
	VersionLabel string
	SubmittedAt  string
	// Participant is empty for anonymous surveys, where no such thing
	// exists to show.
	Participant string
	ResponseID  string
	// AnswerLongish marks answers worth rendering with more room.
	AnswerLongish bool
}
