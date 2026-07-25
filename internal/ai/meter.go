package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/clock"
)

// UsageStore is what the meter needs from storage; internal/store's
// Surveys satisfies it.
type UsageStore interface {
	AddAIUsageRecord(ctx context.Context, workspaceID uuid.UUID, surveyID *uuid.UUID, kind string, tokens int64, estCostEUR float64, durationSecs int, day time.Time) error
	WorkspaceTokensOnDay(ctx context.Context, workspaceID uuid.UUID, day time.Time) (int64, error)
	GlobalCostOnDay(ctx context.Context, day time.Time) (float64, error)
	SurveyVoiceSecondsOnDay(ctx context.Context, surveyID uuid.UUID, day time.Time) (int64, error)
}

var (
	// ErrQuotaExceeded: this workspace has spent its daily allowance
	// (SPEC.md story 21 — one enthusiastic teammate cannot burn the
	// budget).
	ErrQuotaExceeded = errors.New("ai: workspace daily quota exceeded")
	// ErrBreakerTripped: the whole instance has hit its daily € ceiling
	// (story 67 — abuse cannot bankrupt the product). Every AI endpoint
	// refuses until the day rolls over.
	ErrBreakerTripped = errors.New("ai: daily budget breaker tripped")
)

// Meter enforces M6-T2: per-workspace daily token caps and the global
// daily € breaker, both computed from the ai_usage table so restarts and
// multiple instances agree. Callers Check before an AI call and Record
// after it.
type Meter struct {
	Store UsageStore
	Clock clock.Clock
	// WorkspaceDailyTokens caps each workspace per day.
	WorkspaceDailyTokens int64
	// DailyBudgetEUR is the global breaker threshold.
	DailyBudgetEUR float64
	// CostPer1KTokensEUR converts token estimates to cost estimates;
	// tuned to real provider pricing at the cloud milestone.
	CostPer1KTokensEUR float64
	// VoiceSurveyDailySeconds caps how much speech one survey may have
	// transcribed per day (M5-T4). Zero disables the cap.
	VoiceSurveyDailySeconds int
	Logger                  *slog.Logger
}

// audioTokensPerSecond estimates what a second of speech costs a
// multimodal model. Gemini bills audio at roughly this rate; like the
// chars/4 estimate for text it errs high, which is the safe direction for
// a budget guard.
const audioTokensPerSecond = 32

// day truncates to the accounting day (UTC — one unambiguous boundary).
func (m *Meter) day() time.Time {
	return m.Clock.Now().UTC().Truncate(24 * time.Hour)
}

// Check refuses the call before any tokens are spent. Order matters: the
// breaker outranks the quota, because a tripped breaker must present the
// same refusal to everyone.
func (m *Meter) Check(ctx context.Context, workspaceID uuid.UUID) error {
	cost, err := m.Store.GlobalCostOnDay(ctx, m.day())
	if err != nil {
		return err
	}
	if cost >= m.DailyBudgetEUR {
		// This IS the alert until Cloud Monitoring exists (M9-T2): an
		// Error-level line is what the log-based alerting will match.
		m.Logger.Error("AI budget breaker tripped — all AI endpoints disabled until tomorrow",
			"spent_eur", cost, "budget_eur", m.DailyBudgetEUR)
		return ErrBreakerTripped
	}

	tokens, err := m.Store.WorkspaceTokensOnDay(ctx, workspaceID, m.day())
	if err != nil {
		return err
	}
	if tokens >= m.WorkspaceDailyTokens {
		return ErrQuotaExceeded
	}
	return nil
}

// Counted wraps a stream and tallies the characters it delivers, so a
// caller can meter what a model actually produced rather than guessing
// before the fact. Prompt characters are added by the caller, which knows
// what it sent.
//
//	counted := ai.Counted(stream)
//	defer func() { _ = s.aiMeter.Record(ctx, wsID, &id, string(ai.OpGenerate), counted.Chars()+len(prompt)) }()
type CountedStream struct {
	inner Stream
	chars int
}

func Counted(s Stream) *CountedStream { return &CountedStream{inner: s} }

func (c *CountedStream) Recv() (string, error) {
	fragment, err := c.inner.Recv()
	c.chars += len(fragment)
	return fragment, err
}

func (c *CountedStream) Close() error { return c.inner.Close() }

// Chars is the number of characters delivered so far — meaningful even
// when a stream failed midway, which is the case that must still be paid
// for.
func (c *CountedStream) Chars() int { return c.chars }

// Record accounts a completed call. Tokens are estimated from characters
// (~4 chars/token, the industry rule of thumb) until a provider reports
// real counts; overestimating slightly is the safe direction for a
// budget guard.
func (m *Meter) Record(ctx context.Context, workspaceID uuid.UUID, surveyID *uuid.UUID, kind string, chars int) error {
	return m.record(ctx, workspaceID, surveyID, kind, int64(chars/4)+1, 0)
}

// RecordVoice accounts one transcription. Audio is billed by duration
// rather than by the size of the transcript it produced — a minute of
// silence costs the same as a minute of speech.
func (m *Meter) RecordVoice(ctx context.Context, workspaceID uuid.UUID, surveyID *uuid.UUID, seconds, transcriptChars int) error {
	tokens := int64(seconds*audioTokensPerSecond) + int64(transcriptChars/4) + 1
	return m.record(ctx, workspaceID, surveyID, string(OpTranscribe), tokens, seconds)
}

func (m *Meter) record(ctx context.Context, workspaceID uuid.UUID, surveyID *uuid.UUID, kind string, tokens int64, seconds int) error {
	cost := float64(tokens) / 1000 * m.CostPer1KTokensEUR
	if err := m.Store.AddAIUsageRecord(ctx, workspaceID, surveyID, kind, tokens, cost, seconds, m.day()); err != nil {
		return fmt.Errorf("ai: record usage: %w", err)
	}
	// One scrub-safe line per AI call (kind + counts only, never content):
	// Cloud Monitoring's ai_usage anomaly alert (M9-T2) counts these.
	m.Logger.Info("ai usage recorded",
		"kind", kind, "tokens", tokens, "est_cost_eur", cost)
	return nil
}

// VoiceSecondsLeft is how much more speech this survey may have
// transcribed today. It is checked in addition to Check, never instead of
// it: the € breaker and the workspace quota still apply to voice. A
// configured cap of zero means uncapped, and reports as such.
func (m *Meter) VoiceSecondsLeft(ctx context.Context, surveyID uuid.UUID) (int, error) {
	if m.VoiceSurveyDailySeconds <= 0 {
		return math.MaxInt32, nil
	}
	spent, err := m.Store.SurveyVoiceSecondsOnDay(ctx, surveyID, m.day())
	if err != nil {
		return 0, err
	}
	left := m.VoiceSurveyDailySeconds - int(spent)
	if left < 0 {
		return 0, nil
	}
	return left, nil
}
