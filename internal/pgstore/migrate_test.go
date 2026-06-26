package pgstore

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMigrateIsIdempotent(t *testing.T) {
	store := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate (2nd call): %v", err)
	}
}

// TestMigrateIsSafeForConcurrentReplicas guards against the startup race
// every apigateway replica is exposed to: each calls Migrate independently
// on a fresh connection (see cmd/apigateway/main.go), so two replicas
// starting at once must not both try to apply the same migration.
// migrationAdvisoryLockKey serializes them instead.
func TestMigrateIsSafeForConcurrentReplicas(t *testing.T) {
	store := newTestStore(t)

	const concurrency = 5
	var wg sync.WaitGroup
	errs := make([]error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			errs[i] = store.Migrate(ctx)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Migrate (goroutine %d): %v", i, err)
		}
	}
}
