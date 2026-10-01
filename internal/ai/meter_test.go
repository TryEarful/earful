package ai_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TryEarful/earful/internal/ai"
	"github.com/TryEarful/earful/internal/clock"
)

// memoryUsage is an in-memory UsageStore for meter-logic tests; the SQL
// half is covered by internal/store's own test against real Postgres.
type memoryUsage struct {
	mu    sync.Mutex
	rows  []usageRow
	tiers map[uuid.UUID]string
}

func (m *memoryUsage) setTier(workspaceID uuid.UUID, tier string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tiers == nil {
		m.tiers = map[uuid.UUID]string{}
	}
	m.tiers[workspaceID] = tier
}

// WorkspaceAITier answers "normal" for a workspace never set, as the
// column default does.
func (m *memoryUsage) WorkspaceAITier(_ context.Context, workspaceID uuid.UUID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tier, ok := m.tiers[workspaceID]; ok {
		return tier, nil
	}
	return "normal", nil
}

type usageRow struct {
	workspace uuid.UUID
	survey    *uuid.UUID
	kind      string
	tokens    int64
	cost      float64
	seconds   int
	day       time.Time
}

func (m *memoryUsage) AddAIUsageRecord(_ context.Context, workspaceID uuid.UUID, surveyID *uuid.UUID, kind string, tokens int64, cost float64, seconds int, day time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, usageRow{
		workspace: workspaceID, survey: surveyID, kind: kind,
		tokens: tokens, cost: cost, seconds: seconds, day: day,
	})
	return nil
}

func (m *memoryUsage) SurveyVoiceSecondsOnDay(_ context.Context, surveyID uuid.UUID, day time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sum int64
	for _, r := range m.rows {
		if r.survey != nil && *r.survey == surveyID && r.day.Equal(day) {
			sum += int64(r.seconds)
		}
	}
	return sum, nil
}

func (m *memoryUsage) WorkspaceTokensOnDay(_ context.Context, workspaceID uuid.UUID, day time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sum int64
	for _, r := range m.rows {
		if r.workspace == workspaceID && r.day.Equal(day) {
			sum += r.tokens
		}
	}
	return sum, nil
}

func (m *memoryUsage) GlobalCostOnDay(_ context.Context, day time.Time) (float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sum float64
	for _, r := range m.rows {
		if r.day.Equal(day) {
			sum += r.cost
		}
	}
	return sum, nil
}

func newMeter(store ai.UsageStore, c clock.Clock) *ai.Meter {
	return &ai.Meter{
		Store:              store,
		Clock:              c,
		DailyTokens:        ai.TierCaps{LowNormal: 500, Normal: 1000, High: 5000},
		DailyBudgetEUR:     1.0,
		CostPer1KTokensEUR: 0.5,
		Logger:             slog.New(slog.DiscardHandler),
	}
}

// TestMeter_WorkspaceQuotaTrips: one workspace exhausts its cap; another
// workspace is unaffected (story 21).
func TestMeter_WorkspaceQuotaTrips(t *testing.T) {
	t.Parallel()
	store := &memoryUsage{}
	meter := newMeter(store, clock.NewFake(time.Now()))
	ctx := context.Background()
	greedy, frugal := uuid.New(), uuid.New()

	if err := meter.Check(ctx, greedy); err != nil {
		t.Fatalf("fresh workspace refused: %v", err)
	}
	// Spend past the 1000-token cap (~4 chars/token).
	if err := meter.Record(ctx, greedy, nil, "generate", 4100); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := meter.Check(ctx, greedy); !errors.Is(err, ai.ErrQuotaExceeded) {
		t.Errorf("over-cap workspace: err = %v, want ErrQuotaExceeded", err)
	}
	if err := meter.Check(ctx, frugal); err != nil {
		t.Errorf("another workspace was caught in the quota: %v", err)
	}
}

// TestMeter_EachTierHasItsOwnCap: the same spend is over the low normal
// cap, under the normal one, and well under the high one (issue #3).
func TestMeter_EachTierHasItsOwnCap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Each case has a store of its own, so the spend stays under the
	// € breaker. About 700 tokens each: over 500, under 1000 and 5000.
	cases := []struct {
		tier    string
		refused bool
		cap     int64
	}{
		{"low_normal", true, 500},
		{"normal", false, 1000},
		{"high", false, 5000},
	}
	for _, c := range cases {
		store := &memoryUsage{}
		meter := newMeter(store, clock.NewFake(time.Now()))
		workspace := uuid.New()
		store.setTier(workspace, c.tier)
		if err := meter.Record(ctx, workspace, nil, "generate", 2800); err != nil {
			t.Fatalf("record: %v", err)
		}
		err := meter.Check(ctx, workspace)
		if refused := errors.Is(err, ai.ErrQuotaExceeded); refused != c.refused {
			t.Errorf("%s: refused = %v (%v), want %v", c.tier, refused, err, c.refused)
		}
		usage, err := meter.Usage(ctx, workspace)
		if err != nil {
			t.Fatalf("usage: %v", err)
		}
		if string(usage.Tier) != c.tier || usage.Cap != c.cap || usage.Tokens != 701 {
			t.Errorf("%s: usage = %+v, want tier %s, cap %d, 701 tokens", c.tier, usage, c.tier, c.cap)
		}
	}

	// Moving a workspace up a tier lifts the refusal the same day.
	store := &memoryUsage{}
	meter := newMeter(store, clock.NewFake(time.Now()))
	workspace := uuid.New()
	store.setTier(workspace, "normal")
	if err := meter.Record(ctx, workspace, nil, "generate", 4100); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := meter.Check(ctx, workspace); !errors.Is(err, ai.ErrQuotaExceeded) {
		t.Fatalf("over the normal cap: err = %v, want ErrQuotaExceeded", err)
	}
	store.setTier(workspace, "high")
	if err := meter.Check(ctx, workspace); err != nil {
		t.Errorf("after moving to high: %v", err)
	}
}

// TestMeter_BreakerOutranksATier: a high tier is no way past the € breaker.
func TestMeter_BreakerOutranksATier(t *testing.T) {
	t.Parallel()
	store := &memoryUsage{}
	meter := newMeter(store, clock.NewFake(time.Now()))
	ctx := context.Background()
	workspace := uuid.New()
	store.setTier(workspace, "high")

	// 4800 tokens at €0.5/1k is €2.40, past the €1 budget but under the
	// high tier's 5000 tokens.
	if err := meter.Record(ctx, workspace, nil, "generate", 4*4800); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := meter.Check(ctx, workspace); !errors.Is(err, ai.ErrBreakerTripped) {
		t.Errorf("err = %v, want ErrBreakerTripped", err)
	}
}

// TestMeter_BreakerTrips is M6-T2's AC: the global € ceiling disables AI
// for everyone, regardless of individual quotas.
func TestMeter_BreakerTrips(t *testing.T) {
	t.Parallel()
	store := &memoryUsage{}
	meter := newMeter(store, clock.NewFake(time.Now()))
	ctx := context.Background()

	// Many workspaces together cross the €1 budget (each under its own
	// token cap): 3 workspaces × 800 tokens × €0.5/1k ≈ €1.20.
	for i := 0; i < 3; i++ {
		if err := meter.Record(ctx, uuid.New(), nil, "generate", 3200); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if err := meter.Check(ctx, uuid.New()); !errors.Is(err, ai.ErrBreakerTripped) {
		t.Errorf("err = %v, want ErrBreakerTripped for everyone", err)
	}
}

// TestMeter_DayRollsOver: both the quota and the breaker are per-day; the
// fake clock crossing midnight resets them.
func TestMeter_DayRollsOver(t *testing.T) {
	t.Parallel()
	store := &memoryUsage{}
	fake := clock.NewFake(time.Date(2026, 7, 20, 23, 0, 0, 0, time.UTC))
	meter := newMeter(store, fake)
	ctx := context.Background()
	workspace := uuid.New()

	if err := meter.Record(ctx, workspace, nil, "generate", 4100); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := meter.Check(ctx, workspace); !errors.Is(err, ai.ErrQuotaExceeded) {
		t.Fatalf("should be over quota today: %v", err)
	}

	fake.Advance(2 * time.Hour) // past midnight UTC
	if err := meter.Check(ctx, workspace); err != nil {
		t.Errorf("quota should reset with the new day: %v", err)
	}
}

// TestMeter_BreakerLogsTheAlert: until Cloud Monitoring exists, the
// Error-level log line IS the alert (M9-T2 wires it to a channel).
func TestMeter_BreakerLogsTheAlert(t *testing.T) {
	t.Parallel()
	store := &memoryUsage{}
	var logged capturedLog
	meter := newMeter(store, clock.NewFake(time.Now()))
	meter.Logger = slog.New(&logged)
	ctx := context.Background()

	if err := meter.Record(ctx, uuid.New(), nil, "analyze", 10_000); err != nil {
		t.Fatalf("record: %v", err)
	}
	_ = meter.Check(ctx, uuid.New())
	if !logged.hasError("breaker tripped") {
		t.Error("breaker trip did not produce an Error-level alert line")
	}
}

type capturedLog struct {
	mu      sync.Mutex
	entries []slog.Record
}

func (c *capturedLog) Enabled(context.Context, slog.Level) bool { return true }
func (c *capturedLog) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, r)
	return nil
}
func (c *capturedLog) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *capturedLog) WithGroup(string) slog.Handler      { return c }

func (c *capturedLog) hasError(substr string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.entries {
		if r.Level == slog.LevelError && strings.Contains(r.Message, substr) {
			return true
		}
	}
	return false
}

// TestCounted_TalliesWhatTheModelDelivered: metering must charge for the
// output that actually arrived, including output from a stream that then
// failed — the tokens were spent either way.
func TestCounted_TalliesWhatTheModelDelivered(t *testing.T) {
	t.Parallel()
	fake := &ai.Fake{
		GenerateScript: [][]string{{"one ", "two ", "three"}},
		StreamErr:      errors.New("provider hung up"),
		StreamErrAfter: 2,
	}
	stream, err := fake.Generate(context.Background(), ai.GenerateRequest{Prompt: "hi"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	counted := ai.Counted(stream)
	if _, err := ai.Collect(counted); err == nil {
		t.Fatal("a mid-stream failure must surface, not be swallowed")
	}
	if got := counted.Chars(); got != len("one two ") {
		t.Errorf("counted %d chars, want %d (what was delivered before the failure)", got, len("one two "))
	}
}

// TestMeter_VoiceIsBilledByDuration: a transcription's cost follows the
// seconds of speech, not the length of the transcript, and the
// per-survey daily cap is what a respondent runs into first (M5-T4).
func TestMeter_VoiceIsBilledByDuration(t *testing.T) {
	t.Parallel()
	usage := &memoryUsage{}
	clk := clock.NewFake(time.Now())
	meter := newMeter(usage, clk)
	meter.VoiceSurveyDailySeconds = 90

	workspace, survey := uuid.New(), uuid.New()

	left, err := meter.VoiceSecondsLeft(context.Background(), survey)
	if err != nil || left != 90 {
		t.Fatalf("fresh survey has %d seconds left (%v), want 90", left, err)
	}
	if err := meter.RecordVoice(context.Background(), workspace, &survey, 60, len("a short transcript")); err != nil {
		t.Fatalf("RecordVoice: %v", err)
	}
	left, err = meter.VoiceSecondsLeft(context.Background(), survey)
	if err != nil || left != 30 {
		t.Fatalf("after 60s, %d seconds left (%v), want 30", left, err)
	}
	if err := meter.RecordVoice(context.Background(), workspace, &survey, 45, 0); err != nil {
		t.Fatalf("RecordVoice: %v", err)
	}
	if left, _ := meter.VoiceSecondsLeft(context.Background(), survey); left != 0 {
		t.Errorf("an exhausted cap reports %d seconds left, want 0", left)
	}

	// A minute of speech must cost far more than a minute of transcript
	// would as text, or voice would be effectively unmetered.
	tokens, err := usage.WorkspaceTokensOnDay(context.Background(), workspace, clk.Now().UTC().Truncate(24*time.Hour))
	if err != nil {
		t.Fatalf("WorkspaceTokensOnDay: %v", err)
	}
	if tokens < int64(105*32) {
		t.Errorf("105 seconds of audio billed %d tokens; duration is not driving the estimate", tokens)
	}

	// Another survey's voice budget is its own.
	other := uuid.New()
	if left, _ := meter.VoiceSecondsLeft(context.Background(), other); left != 90 {
		t.Errorf("a different survey has %d seconds left, want a full 90", left)
	}
}

// TestMeter_VoiceCapCanBeDisabled: self-hosters running a local whisper
// pay nothing per second and should not be rationed.
func TestMeter_VoiceCapCanBeDisabled(t *testing.T) {
	t.Parallel()
	meter := newMeter(&memoryUsage{}, clock.NewFake(time.Now()))
	meter.VoiceSurveyDailySeconds = 0
	left, err := meter.VoiceSecondsLeft(context.Background(), uuid.New())
	if err != nil || left < 3600 {
		t.Errorf("uncapped survey reports %d seconds (%v), want effectively unlimited", left, err)
	}
}
