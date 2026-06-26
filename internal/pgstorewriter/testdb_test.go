package pgstorewriter

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/pgstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultTestDSN mirrors internal/pgstore's test DSN: the postgres
// service container CI and docker-compose.yml both provide.
const defaultTestDSN = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

func testDSN() string {
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return defaultTestDSN
}

func newTestStore(t *testing.T) *pgstore.Store {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := pgstore.Connect(ctx, testDSN())
	if err != nil {
		t.Skipf("postgres unavailable, skipping integration test: %v", err)
	}
	t.Cleanup(store.Close)

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	return store
}

// newTestQueryPool opens a second, direct connection pool for tests to
// verify rows that HistoryWriter/EventWriter wrote through the *pgstore.Store
// they were given, which intentionally doesn't expose its pool for
// production callers to reach around.
func newTestQueryPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDSN())
	if err != nil {
		t.Skipf("postgres unavailable, skipping integration test: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
