package main

import (
	"context"
	"fmt"
	"os"

	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/logging"
	"github.com/TryEarful/earful/internal/store"
)

func runMigrate(_ context.Context, _ []string) int {
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

	if err := store.Migrate(cfg.DatabaseURL); err != nil {
		logger.Error("migrate: failed", "error", err)
		return 1
	}
	logger.Info("migrate: up complete")
	return 0
}
