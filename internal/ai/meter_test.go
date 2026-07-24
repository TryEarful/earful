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
	mu   sync.Mutex
	rows []usageRow
}

type usageRow struct {
	workspace uuid.UUID
	tokens    int64
	cost      float64
	day       time.Time
}

func (m *memoryUsage) AddAIUsageRecord(_ context.Context, workspaceID uuid.UUID, _ *uuid.UUID, _ string, tokens int64, cost float64, day time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = append(m.rows, usageRow{workspace: workspaceID, tokens: tokens, cost: cost, day: day})
	return nil
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
		Store:                store,
		Clock:                c,
		WorkspaceDailyTokens: 1000,
		DailyBudgetEUR:       1.0,
		CostPer1KTokensEUR:   0.5,
		Logger:               slog.New(slog.DiscardHandler),
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
