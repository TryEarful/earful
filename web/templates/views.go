package templates

import (
	"hash/fnv"

	"github.com/TryEarful/earful/internal/domain"
)

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
	// StarterNote is set for a Starter Survey its owner has not yet
	// published a version of: the card then says what the survey is and
	// that it is theirs to change (story 86).
	StarterNote bool
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

// VoiceIndex is the survey's colour, 1 to 10: a Voice colour from the
// palette (docs/style-guide.md), for telling surveys apart at a glance.
// It is worked out from the ID, so it never changes and needs nothing
// stored; two surveys may share one, which costs nothing, since the
// colour means nothing beyond "this one".
func (s SurveyView) VoiceIndex() int {
	h := fnv.New32a()
	h.Write([]byte(s.ID))
	return int(h.Sum32()%10) + 1
}

// CardVoices are the colours of a list of survey cards: each survey's
// own, except where it would match the card above, which takes the next
// colour instead, so neighbours are always told apart.
func CardVoices(list []SurveyView) []int {
	out := make([]int, len(list))
	for i, s := range list {
		v := s.VoiceIndex()
		if i > 0 && v == out[i-1] {
			v = v%10 + 1
		}
		out[i] = v
	}
	return out
}

type SurveyListData struct {
	Surveys []SurveyView
}

// NewSurveyData is the new-survey form, with what was typed into it
// kept when the form comes back with an error.
type NewSurveyData struct {
	// AIEnabled offers the description from which questions, and a
	// title when none is typed, are drafted.
	AIEnabled bool
	Title     string
	Prompt    string
	Anonymous bool
	CloseAt   string
	Error     string
}

// ParticipantView is one row of the participants list.
type ParticipantView struct {
	Email  string
	Status string
}

type SurveyEditorData struct {
	// Origin is this instance's public address, so the share link can be
	// shown whole, as it is pasted into an email or a chat.
	Origin string
	// DraftChanged is whether publishing would make a new version; the
	// editor offers Publish only then.
	DraftChanged  bool
	Survey        SurveyView
	Questions     []domain.Question
	Versions      []VersionView
	ResponseCount int
	// AIEnabled shows the "draft with AI" panel. False when no text
	// provider is configured: an absent capability is an absent feature,
	// not a button that fails (Appendix D).
	AIEnabled bool
	// GeneratePrompt refills the drafting panel after a refused run.
	GeneratePrompt string
	// Participants is populated for invited surveys only.
	Participants []ParticipantView
	PendingCount int
	// Thanks is what the thank you page form shows: the draft's, or what
	// was just typed when it was refused.
	Thanks ThanksFormView
	Error  string
	Notice string
}

// ThanksFormView is the editor's thank you page form.
type ThanksFormView struct {
	Message   string
	LinkLabel string
	LinkURL   string
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
	Insight       InsightView
	Notice        string
	// CanTranslate offers on-demand answer translation; TranslateLang is
	// the language currently shown beside the originals (M11-T2).
	CanTranslate  bool
	TranslateLang string
	TranslateName string
	// Table is story 58's tabular view: one row per response, the same
	// shape the CSV exports.
	TableHeaders []string
	Table        []ResponseRowView
}

// ResponseRowView is one response as a table row.
type ResponseRowView struct {
	ID           string
	SubmittedAt  string
	VersionLabel string
	Participant  string
	Cells        []string
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
	// OtherTexts are what respondents wrote beside Other on a choice
	// question, listed under its bars.
	OtherTexts []TextAnswerView
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
	// Translation is a cached machine translation, shown beside the
	// original and never instead of it (stories 26, 27).
	Translation      string
	TranslationModel string
}

// AccountData is the account page. It grew a struct when the workspace
// export arrived: six positional strings was already one too many.
type AccountData struct {
	IsSuperAdmin bool
	// HasPassword shows the change of address, which is confirmed with
	// the current password.
	HasPassword bool
	// WorkspaceNotice and WorkspaceError belong to the rename form.
	WorkspaceNotice string
	WorkspaceError  string
	// EmailNotice/EmailError belong to the change-email form.
	EmailNotice string
	EmailError  string
	Notice      string
	Export      ExportView
	// AIUsage is today's AI spend against the workspace's allowance,
	// in words; empty on an instance with no AI.
	AIUsage string
}

// AITiersData is the super-admin AI tier control (issue #3).
type AITiersData struct {
	Email      string
	Searched   bool
	Workspaces []AITierWorkspace
	// Options are the tiers in order, each labelled with its cap.
	Options []AITierOption
	Notice  string
	Error   string
}

type AITierWorkspace struct {
	ID    string
	Name  string
	Tier  string
	Usage string
}

type AITierOption struct {
	Value string
	Label string
}

// SuspensionsData is the operator's suspension page (ADR-0018): the
// suspended workspaces, and the workspaces of the account searched for.
type SuspensionsData struct {
	Suspended []SuspendedWorkspace
	Email     string
	Searched  bool
	Found     []FoundWorkspace
	Notice    string
	Error     string
	// ReasonFor is the workspace whose reason was refused, and Reason
	// what was written, so the form shows it again.
	ReasonFor string
	Reason    string
}

type SuspendedWorkspace struct {
	ID     string
	Name   string
	Member string
	Since  string
	By     string
	Reason string
}

type FoundWorkspace struct {
	ID        string
	Name      string
	Suspended bool
}

// ExportView is the state of the workspace export (M7-T3): building,
// ready with an expiring link, or failed with a readable reason.
type ExportView struct {
	Status       string
	Building     bool
	Ready        bool
	Failed       bool
	Error        string
	SizeLabel    string
	FinishedAt   string
	ExpiresAt    string
	DownloadPath string
}

// --- erasure fast-path (M8-T3) -------------------------------------------

// ErasureData is the support-only erasure page.
type ErasureData struct {
	Searched bool
	Subject  SubjectView
	Done     bool
	// ErasedRow is the number of rows removed — counts only, because an
	// erasure record naming the person erased would defeat the point.
	ErasedRow int64
}

// SubjectView is what will be erased, shown before anything is.
type SubjectView struct {
	Email         string
	Found         bool
	HasAccount    bool
	Workspaces    int
	Surveys       int
	ParticipantIn int
	Responses     int
	Suppressed    bool
}

// InsightView is an Insight Summary as displayed (M10). Every field
// except Output exists to keep analysis from passing for data: the model
// that wrote it, when, over how many responses, and whether responses
// have arrived since.
type InsightView struct {
	// Available is false when no text AI is configured at all, in which
	// case the panel is not offered.
	Available     bool
	Present       bool
	Output        string
	Model         string
	GeneratedAt   string
	ResponseCount int
	CountLabel    string
	Stale         bool
	StaleNote     string
}

// --- localization (M11-T1) -----------------------------------------------

// LocalizationsData is the translation workspace for one survey.
type LocalizationsData struct {
	Survey    SurveyView
	Languages []LanguageView
	Questions []domain.Question
	// CanTranslate is false when no text AI is configured: the language
	// list still works, translations are then written by hand.
	CanTranslate bool
	Error        string
	Notice       string
}

// LanguageView is one language and how far along it is.
type LanguageView struct {
	Code string
	// Name is the language as a sentence names it, and Label as it
	// stands on its own: a heading, a field's label.
	Name         string
	Label        string
	Total        int
	Reviewed     int
	PendingCount int
	// Ready means every question is translated and reviewed against the
	// current wording — the condition publishing requires.
	Ready     bool
	Questions []LocalizedQuestionView
	// Thanks is the thank you page in this language, nil when the
	// creator has written none to translate.
	Thanks *LocalizedThanksView
	// Style is the words of the survey's style in this language, nil
	// when the style has none to translate.
	Style *LocalizedStyleView
}

// LocalizedStyleView is the words of a survey's style in one language,
// beside the creator's wording: the tagline, the footer's text and each
// link's label. A part the source does not have is not offered.
type LocalizedStyleView struct {
	Source   domain.StyleWords
	Words    domain.StyleWords
	Reviewed bool
	Stale    bool
}

// label is the translated label of the link at a position, or empty.
func label(labels []string, i int) string {
	if i < len(labels) {
		return labels[i]
	}
	return ""
}

// LocalizedThanksView is the thank you page in one language, beside the
// creator's wording. A part the source does not have is not offered.
type LocalizedThanksView struct {
	SourceMessage   string
	SourceLinkLabel string
	Message         string
	LinkLabel       string
	Reviewed        bool
	Stale           bool
}

// LocalizedQuestionView is one question in one language, beside its
// source, so a reviewer can compare rather than trust.
type LocalizedQuestionView struct {
	IdentityID    string
	SourceText    string
	Text          string
	Options       string
	SourceOptions string
	NeedsOptions  bool
	Reviewed      bool
	// Stale marks a translation of a wording the creator has since
	// changed: reviewed once, but not for what the question says now.
	Stale bool
}

// LanguageChoice is one option in the respondent's language picker.
// Suggested marks the browser's own preference — a suggestion, never a
// selection made for them. Lang is the language Name is written in when
// it is the name the language gives itself, so a screen reader says it
// as its speakers do; it is empty when Name is in the page's language.
type LanguageChoice struct {
	Code      string
	Name      string
	Lang      string
	Selected  bool
	Suggested bool
}

// --- founder metrics (M9-T7) ---------------------------------------------

// MetricsData is the super-admin metrics page: first-party numbers, read
// from this instance's own database, with nothing added to a respondent
// page (ADR-0006).
type MetricsData struct {
	WindowDays  int
	Totals      []MetricTotal
	Signups     []MetricPoint
	Responses   []MetricPoint
	AICost      []MetricPoint
	AICostTotal string
	BudgetNote  string
}

type MetricTotal struct {
	Label string
	Value string
}

type MetricPoint struct {
	Day   string
	Value string
}

// --- stats page (issue #2, ADR-0012) -------------------------------------

// SurveyStatsData is the stats page, fully formatted. Everything with a
// date honours Range; the audience section is undated by design.
type SurveyStatsData struct {
	Survey SurveyView
	Range  StatsRangeView
	CSVURL string

	// Big picture.
	Opened         string
	Submissions    string
	CompletionRate string
	// PerDay is the mean of submissions a day over the range, and
	// PerDayNote says how many days that mean is taken over.
	PerDay         string
	PerDayNote     string
	TimeToComplete string
	TimedNote      string
	// LumpNote explains counts from before per-day tracking, when any.
	LumpNote string

	// Trend is every day in range, zero-filled, for the chart script;
	// TrendRows is only the days with something on them, for the table
	// a browser without scripts reads.
	Trend     []TrendPoint
	TrendRows []TrendPoint

	// Questions is where answers stop, one row per Question Identity in
	// current order.
	Questions []QuestionStopView
	StopsNote string

	// Audience: ADR-0009's three coarse facts, suppressed below five.
	Browsers        []CountView
	Devices         []CountView
	Countries       []CountView
	HasAudience     bool
	SuppressionNote string
}

// StatsRangeView is the range control's state.
type StatsRangeView struct {
	FromInput string
	ToInput   string
	MinInput  string
	MaxInput  string
	Label     string
	Preset    string
	AllTime   bool
}

// TrendPoint is one day on the chart.
type TrendPoint struct {
	Day         string `json:"day"`
	Label       string `json:"label"`
	Opened      int    `json:"opened"`
	Submissions int    `json:"submissions"`
}

// QuestionStopView is one row of the question-by-question table.
type QuestionStopView struct {
	Text      string
	TypeLabel string
	Required  bool
	Stopped   int
	Percent   int
	Share     string
}
