// Package store holds the pgx/sqlc-generated data access layer plus goose
// migration wiring shared by the migrate subcommand and the test harness.
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver for goose
	"github.com/pressly/goose/v3"

	"github.com/TryEarful/earful/db/migrations"
)

// migrateLockKey is an arbitrary constant identifying "earful schema
// migration" for pg_advisory_lock.
const migrateLockKey = 794_215_037

// Migrate applies all pending goose migrations embedded in db/migrations
// against dsn. It is idempotent, and safe to run concurrently: a
// session-scoped advisory lock serializes migrators, so parallel test
// packages — and later, multiple Cloud Run instances racing at deploy —
// never interleave DDL (which surfaces as pg_type unique-violation
// errors, not as a clean "already applied").
func Migrate(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("store: open db: %w", err)
	}
	defer db.Close()

	// The advisory lock is session-scoped, so it must live on one pinned
	// connection for the whole migration; goose itself may use any.
	ctx := context.Background()
	lockConn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: acquire lock conn: %w", err)
	}
	defer lockConn.Close()
	if _, err := lockConn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrateLockKey); err != nil {
		return fmt.Errorf("store: acquire migration lock: %w", err)
	}
	defer lockConn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrateLockKey) //nolint:errcheck // session end releases it regardless

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("store: set dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("store: migrate up: %w", err)
	}
	return nil
}
