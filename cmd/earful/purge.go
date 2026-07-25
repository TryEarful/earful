package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/logging"
	"github.com/TryEarful/earful/internal/purge"
)

// runPurge hard-deletes what has been soft-deleted for 30 days, expires
// tokens and trims short-retention logs (M8-T2). It is the same binary
// the server runs from: `make purge` locally, a Cloud Scheduler job in
// production.
//
// --dry-run does the whole thing and rolls back, so the numbers it
// prints are the numbers a real run would produce.
func runPurge(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("purge", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "report what would be deleted, then roll back")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.LoadJob()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := cfg.RequireDatabaseURL(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	logger := logging.New(cfg.LogLevel, os.Stdout)

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("purge: cannot connect", "error", err)
		return 1
	}
	defer pool.Close()

	started := time.Now()
	report, err := purge.Run(ctx, pool, time.Now(), *dryRun)
	if err != nil {
		logger.Error("purge failed", "error", err)
		return 1
	}

	// Counts only, never subjects: a purge log naming the people it
	// erased would be its own retention problem.
	for _, step := range report.Order {
		if count := report.Counts[step]; count > 0 {
			logger.Info("purge step", "step", step, "rows", count, "dry_run", report.DryRun)
		}
	}
	logger.Info("purge complete",
		"rows", report.Total(),
		"dry_run", report.DryRun,
		"duration_ms", time.Since(started).Milliseconds())
	return 0
}
