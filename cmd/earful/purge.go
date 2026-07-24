package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/logging"
)

// runPurge is a stub: real retention cleanup arrives in M8. It refuses to
// run at all without --dry-run, so nobody mistakes the stub for working
// deletion.
func runPurge(_ context.Context, args []string) int {
	fs := flag.NewFlagSet("purge", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "run without deleting anything")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	logger := logging.New(cfg.LogLevel, os.Stdout)

	if !*dryRun {
		logger.Warn("purge: real purge not implemented yet (arrives M8); refusing to run without --dry-run")
		return 1
	}

	logger.Info("purge: dry run, nothing to purge yet (M8 implements deletion)")
	return 0
}
