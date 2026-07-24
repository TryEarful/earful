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
