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
	// WorkspaceAITier is the workspace's tier as stored; the meter maps
	// it onto a cap.
	WorkspaceAITier(ctx context.Context, workspaceID uuid.UUID) (string, error)
}

// Tier names a workspace's daily AI allowance. The stored values match
// the CHECK constraint on workspaces.ai_tier.
type Tier string

const (
	TierLowNormal Tier = "low_normal"
	TierNormal    Tier = "normal"
	TierHigh      Tier = "high"
)

// Tiers lists every tier from the smallest allowance to the largest.
var Tiers = []Tier{TierLowNormal, TierNormal, TierHigh}

// ParseTier accepts exactly the stored spellings.
func ParseTier(s string) (Tier, bool) {
	for _, t := range Tiers {
		if string(t) == s {
			return t, true
		}
	}
	return "", false
}

// TierCaps holds each tier's daily token cap.
type TierCaps struct {
	LowNormal int64
	Normal    int64
	High      int64
}

// For returns the cap of a tier. An unrecognised tier gets the normal
// cap: the database constraint makes that unreachable, and the normal
// cap is what every workspace had before tiers existed.
func (c TierCaps) For(t Tier) int64 {
	switch t {
	case TierLowNormal:
		return c.LowNormal
	case TierHigh:
		return c.High
	default:
		return c.Normal
	}
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

// Meter enforces M6-T2: per-workspace daily token caps, chosen by the
// workspace's tier (issue #3), and the global
// daily € breaker, both computed from the ai_usage table so restarts and
// multiple instances agree. Callers Check before an AI call and Record
// after it.
type Meter struct {
	Store UsageStore
	Clock clock.Clock
	// DailyTokens caps each workspace per day, by the workspace's tier.
	DailyTokens TierCaps
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
	return m.CheckFor(ctx, workspaceID, 0)
}

// CheckFor is Check for a call whose input is known to be large before
// it is made, such as one carrying attached files: it refuses when
// today's spend plus estimatedTokens would pass the cap or the breaker,
// so one upload cannot overrun an allowance by more than its own size.
// With an estimate of zero it is exactly Check.
func (m *Meter) CheckFor(ctx context.Context, workspaceID uuid.UUID, estimatedTokens int64) error {
	cost, err := m.Store.GlobalCostOnDay(ctx, m.day())
	if err != nil {
		return err
	}
	estimatedCost := float64(estimatedTokens) / 1000 * m.CostPer1KTokensEUR
	if cost >= m.DailyBudgetEUR || (estimatedTokens > 0 && cost+estimatedCost > m.DailyBudgetEUR) {
		// This IS the alert until Cloud Monitoring exists (M9-T2): an
		// Error-level line is what the log-based alerting will match.
		m.Logger.Error("AI budget breaker tripped — all AI endpoints disabled until tomorrow",
			"spent_eur", cost, "budget_eur", m.DailyBudgetEUR)
		return ErrBreakerTripped
	}

	usage, err := m.Usage(ctx, workspaceID)
	if err != nil {
		return err
	}
	if usage.Tokens >= usage.Cap || (estimatedTokens > 0 && usage.Tokens+estimatedTokens > usage.Cap) {
		return ErrQuotaExceeded
	}
	return nil
}

// Attachment token estimates. Text is charged like every other prompt,
// by characters. Gemini bills an image as 258 tokens per 768px tile and
// a PDF page as 258 tokens plus its text; neither size is known without
// decoding the file, so both are estimated from bytes at rates that err
// high for ordinary files: an image as one tile per 200 KB, a PDF as one
// token per 16 bytes, which covers a text-dense page.
const (
	imageTileTokens = 258
	imageTileBytes  = 200 << 10
	pdfBytesToken   = 16
)

// EstimateTokens is what a request's attachments are expected to cost a
// model, before the call. It is charged with the call's own text, so
// the estimate checked is the amount recorded.
func EstimateTokens(attachments []Attachment) int64 {
	var tokens int64
	for _, a := range attachments {
		size := int64(len(a.Data))
		switch a.MIME {
		case MIMEPNG, MIMEJPEG:
			tokens += imageTileTokens * (size/imageTileBytes + 1)
		case MIMEPDF:
			tokens += imageTileTokens + size/pdfBytesToken
		default:
			tokens += size/4 + 1
		}
	}
	return tokens
}

// WorkspaceUsage is where a workspace stands against its allowance today.
type WorkspaceUsage struct {
	Tier   Tier
	Tokens int64
	Cap    int64
}

// Usage reads a workspace's tier and today's spend. It is what Check
// decides by, so a page that shows it shows the number the meter uses.
func (m *Meter) Usage(ctx context.Context, workspaceID uuid.UUID) (WorkspaceUsage, error) {
	stored, err := m.Store.WorkspaceAITier(ctx, workspaceID)
	if err != nil {
		return WorkspaceUsage{}, err
	}
	tier, ok := ParseTier(stored)
	if !ok {
		tier = TierNormal
	}
	tokens, err := m.Store.WorkspaceTokensOnDay(ctx, workspaceID, m.day())
	if err != nil {
		return WorkspaceUsage{}, err
	}
	return WorkspaceUsage{Tier: tier, Tokens: tokens, Cap: m.DailyTokens.For(tier)}, nil
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

// RecordWith accounts a call that also carried attachments: the
// characters as Record counts them, plus the attachments' estimate
// (EstimateTokens), the same figure CheckFor was asked about.
func (m *Meter) RecordWith(ctx context.Context, workspaceID uuid.UUID, surveyID *uuid.UUID, kind string, chars int, attachmentTokens int64) error {
	return m.record(ctx, workspaceID, surveyID, kind, int64(chars/4)+1+attachmentTokens, 0)
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
