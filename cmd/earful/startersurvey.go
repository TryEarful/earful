package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/starter"
	"github.com/TryEarful/earful/internal/store"
	"github.com/TryEarful/earful/internal/uitext"
)

// runStarterSurvey gives a workspace the Starter Survey (story 86,
// ADR-0015): the published survey every workspace is now created with.
// It is for a workspace made before that, and for an owner who deleted
// theirs and wants it back. The survey is written as a new workspace's
// is, from the same definition, and is the owner's from then on.
//
//	earful starter-survey add <owner-email>
func runStarterSurvey(ctx context.Context, args []string) int {
	if len(args) != 2 || args[0] != "add" {
		fmt.Fprintln(os.Stderr, "usage: earful starter-survey add <owner-email>")
		return 2
	}
	owner := args[1]

	cfg, err := config.LoadJob()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := cfg.RequireDatabaseURL(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "starter-survey: connect:", err)
		return 1
	}
	defer pool.Close()

	title, draft, err := starter.Survey(uitext.Embedded())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	survey, err := store.NewSurveys(pool).AddStarterSurvey(ctx, owner, title, draft, time.Now())
	switch {
	case errors.Is(err, store.ErrOwnerUnknown):
		fmt.Fprintf(os.Stderr, "starter-survey: no account has the address %s, or it has no workspace\n", owner)
		return 1
	case errors.Is(err, store.ErrStarterExists):
		fmt.Fprintf(os.Stderr, "starter-survey: the workspace of %s already holds a starter survey\n", owner)
		return 1
	case err != nil:
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	address := strings.TrimSuffix(cfg.BaseURL, "/") + "/s/" + survey.ID.String()
	fmt.Printf("%s: starter survey published\n", owner)
	fmt.Println("  " + address)
	fmt.Println("  " + address + "?lang=" + starter.Translated)
	return 0
}
