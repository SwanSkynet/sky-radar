package pgstore

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

// migrationAdvisoryLockKey is an arbitrary fixed key used to serialize
// Migrate via Postgres session-level advisory locking. Every apigateway
// replica calls Migrate on startup (see cmd/apigateway/main.go), and
// without this lock two replicas starting concurrently could both see a
// migration as unapplied and race to apply it twice.
const migrationAdvisoryLockKey = 8743

// Migrate applies every embedded migration that has not already been
// recorded in schema_migrations, in filename order. It is idempotent and
// safe to call on every process startup, mirroring how
// natsutil.EnsureFlightsUpdatesStream is safe to call from any consumer at
// startup regardless of which process happens to run first — concurrent
// callers are serialized via migrationAdvisoryLockKey rather than left to
// race.
func (s *Store) Migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("pgstore: acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLockKey); err != nil {
		return fmt.Errorf("pgstore: acquire migration lock: %w", err)
	}
	defer func() {
		// Use a fresh context for the unlock: ctx may already be canceled
		// by the time this runs, which would skip pg_advisory_unlock and
		// leave the session lock held on a connection that goes back to
		// the pool. If the unlock still fails for some other reason, close
		// the connection outright so the lock can't be retained.
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationAdvisoryLockKey); err != nil {
			_ = conn.Conn().Close(unlockCtx)
		}
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("pgstore: create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		return fmt.Errorf("pgstore: read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
		).Scan(&applied); err != nil {
			return fmt.Errorf("pgstore: check migration %s: %w", name, err)
		}
		if applied {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile(migrationsDir + "/" + name)
		if err != nil {
			return fmt.Errorf("pgstore: read migration %s: %w", name, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("pgstore: begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("pgstore: apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("pgstore: record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("pgstore: commit migration %s: %w", name, err)
		}
	}
	return nil
}
