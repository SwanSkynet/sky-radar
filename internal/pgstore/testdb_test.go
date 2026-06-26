package pgstore

import (
	"context"
	"os"
	"testing"
	"time"
)

// defaultTestDSN matches the postgres service container the CI workflow
// runs alongside `go test` (see .github/workflows/ci.yml) and the
// `postgres` service in docker-compose.yml, so these integration tests run
// for real in both places. TEST_DATABASE_URL overrides it for any other
// environment.
const defaultTestDSN = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

// newTestStore connects to a real Postgres instance for integration
// testing, skipping the test if one isn't reachable. Postgres has no
// pure-Go embeddable test double (unlike the in-process NATS server
// internal/natsutil's tests use), so these tests are opt-in via
// availability rather than always-on.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := Connect(ctx, dsn)
	if err != nil {
		t.Skipf("postgres unavailable, skipping integration test: %v", err)
	}
	t.Cleanup(store.Close)

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	return store
}
