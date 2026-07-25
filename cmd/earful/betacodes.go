package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TryEarful/earful/internal/auth"
	"github.com/TryEarful/earful/internal/clock"
	"github.com/TryEarful/earful/internal/config"
	"github.com/TryEarful/earful/internal/email"
)

// runBetaCodes manages private-beta invite codes (M12) with direct
// database access — the operator's bootstrap path before any super admin
// exists, and a fallback forever after. Plaintext codes print exactly
// once; the database keeps hashes.
//
//	earful beta-codes add [-n N] [-label text]
//	earful beta-codes list
//	earful beta-codes revoke <id>
func runBetaCodes(ctx context.Context, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: earful beta-codes <add|list|revoke> [flags]")
		return 2
	}
	verb, rest := args[0], args[1:]

	svc, pool, err := betaService(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pool.Close()

	switch verb {
	case "add":
		fs := flag.NewFlagSet("beta-codes add", flag.ContinueOnError)
		n := fs.Int("n", 1, "how many codes to mint (1-100)")
		label := fs.String("label", "", "label recorded on each code")
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		if *n < 1 || *n > 100 {
			fmt.Fprintln(os.Stderr, "beta-codes: -n must be 1-100")
			return 2
		}
		codes, err := svc.MintBetaCodes(ctx, *n, *label)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("New invite codes — shown once, store them safely:")
		for _, c := range codes {
			fmt.Println("  " + c)
		}
		return 0

	case "list":
		rows, err := svc.ListBetaCodes(ctx)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if len(rows) == 0 {
			fmt.Println("no codes yet — mint with: earful beta-codes add")
			return 0
		}
		for _, c := range rows {
			status := "unused"
			switch {
			case c.RevokedAt != nil:
				status = "revoked"
			case c.UsedAt != nil:
				status = "used"
				if c.UsedByEmail != nil {
					status = "used by " + *c.UsedByEmail
				}
			}
			fmt.Printf("%s  %-10s  %s  %s\n", c.ID, c.CreatedAt.Format("2006-01-02"), status, c.Label)
		}
		return 0

	case "revoke":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "usage: earful beta-codes revoke <id>")
			return 2
		}
		id, err := uuid.Parse(rest[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "beta-codes: not a code id:", rest[0])
			return 2
		}
		if err := svc.RevokeBetaCode(ctx, id); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("revoked")
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown beta-codes subcommand %q\n", verb)
		return 2
	}
}

// runAdmin grants or revokes the instance-level super-admin flag — the
// only path that can; no web surface reaches it (M12).
//
//	earful admin grant <email>
//	earful admin revoke <email>
func runAdmin(ctx context.Context, args []string) int {
	if len(args) != 2 || (args[0] != "grant" && args[0] != "revoke") {
		fmt.Fprintln(os.Stderr, "usage: earful admin <grant|revoke> <email>")
		return 2
	}

	svc, pool, err := betaService(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer pool.Close()

	if err := svc.SetSuperAdmin(ctx, args[1], args[0] == "grant"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%s: super admin %sed\n", args[1], args[0])
	return 0
}

func betaService(ctx context.Context) (*auth.Service, *pgxpool.Pool, error) {
	cfg, err := config.LoadJob()
	if err != nil {
		return nil, nil, err
	}
	if err := cfg.RequireDatabaseURL(); err != nil {
		return nil, nil, err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("beta-codes: connect: %w", err)
	}
	// The console sender is a placeholder — nothing in these subcommands
	// sends email (that being the entire point of M12).
	return auth.NewService(pool, clock.Real{}, email.NewConsole(os.Stdout), cfg.BaseURL), pool, nil
}
