package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/apptest"
	"github.com/TryEarful/earful/internal/store"
)

// TestAIUsage_SumsPerWorkspaceAndGlobally covers the SQL half of M6-T2's
// accounting (the meter logic itself is unit-tested in internal/ai).
func TestAIUsage_SumsPerWorkspaceAndGlobally(t *testing.T) {
	t.Parallel()
	dsn := apptest.NewDB(t)
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	surveys := store.NewSurveys(pool)
	ctx := context.Background()

	var workspaceA, workspaceB uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces (name) VALUES ('ai-usage-a') RETURNING id`).Scan(&workspaceA); err != nil {
		t.Fatalf("workspace a: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspaces (name) VALUES ('ai-usage-b') RETURNING id`).Scan(&workspaceB); err != nil {
		t.Fatalf("workspace b: %v", err)
	}

	// Isolation note (docs/testing.md): global sums span the shared
	// database, so this test claims a day nothing else uses — unique per
	// RUN, not merely per test: a fixed date would accumulate rows across
	// repeated runs and the sums would drift (which is exactly how the
	// first version of this test failed on its second execution).
	day := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, int(time.Now().UnixNano()%3650)*2)
	otherDay := day.AddDate(0, 0, 1)

	baseline, err := surveys.GlobalCostOnDay(ctx, day)
	if err != nil {
		t.Fatalf("baseline cost: %v", err)
	}

	for _, usage := range []store.AIUsage{
		{WorkspaceID: workspaceA, Kind: "generate", Tokens: 100, EstCostEUR: 0.10, Day: day},
		{WorkspaceID: workspaceA, Kind: "translate", Tokens: 50, EstCostEUR: 0.05, Day: day},
		{WorkspaceID: workspaceB, Kind: "analyze", Tokens: 30, EstCostEUR: 0.03, Day: day},
		{WorkspaceID: workspaceA, Kind: "generate", Tokens: 999, EstCostEUR: 0.99, Day: otherDay},
	} {
		if err := surveys.AddAIUsage(ctx, usage); err != nil {
			t.Fatalf("add usage: %v", err)
		}
	}

	tokensA, err := surveys.WorkspaceTokensOnDay(ctx, workspaceA, day)
	if err != nil {
		t.Fatalf("workspace tokens: %v", err)
	}
	if tokensA != 150 {
		t.Errorf("workspace A tokens = %d, want 150 (other workspaces and days excluded)", tokensA)
	}

	cost, err := surveys.GlobalCostOnDay(ctx, day)
	if err != nil {
		t.Fatalf("global cost: %v", err)
	}
	if delta := cost - baseline; delta < 0.17 || delta > 0.19 {
		t.Errorf("global cost delta = %f, want ≈0.18 (A + B on that day only)", delta)
	}
}
