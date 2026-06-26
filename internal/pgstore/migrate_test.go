package pgstore

import (
	"context"
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
