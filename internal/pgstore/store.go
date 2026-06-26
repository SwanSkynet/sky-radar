package pgstore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps the shared Postgres connection pool used for durable history
// and event persistence. See docs/tech-stack/data-and-messaging.md.
type Store struct {
	pool *pgxpool.Pool
}

// Connect dials Postgres at dsn and verifies connectivity with a ping.
// Callers should treat a failure here like a Redis ping failure elsewhere
// in this codebase: fail fast at startup rather than serving traffic
// without a working durable store.
func Connect(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgstore: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgstore: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Ping verifies connectivity to Postgres.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}
