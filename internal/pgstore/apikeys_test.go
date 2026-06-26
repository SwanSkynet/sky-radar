package pgstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func testAPIKey(label, tier string) APIKey {
	return APIKey{
		ID:        flightmodel.NewID(),
		KeyHash:   flightmodel.NewID(), // any unique string stands in for a real sha256 hash here
		Label:     label,
		Tier:      tier,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
}

func TestInsertAPIKeyAndLookupByHash(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := testAPIKey("test key", "elevated")
	if err := store.InsertAPIKey(ctx, key); err != nil {
		t.Fatalf("InsertAPIKey: %v", err)
	}

	got, err := store.LookupAPIKeyByHash(ctx, key.KeyHash)
	if err != nil {
		t.Fatalf("LookupAPIKeyByHash: %v", err)
	}
	if got.ID != key.ID || got.Tier != key.Tier || got.Label != key.Label {
		t.Fatalf("LookupAPIKeyByHash = %+v, want fields matching %+v", got, key)
	}
	if got.RevokedAt != nil {
		t.Fatalf("RevokedAt = %v, want nil for a freshly issued key", got.RevokedAt)
	}
}

func TestLookupAPIKeyByHashNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := store.LookupAPIKeyByHash(ctx, "does-not-exist")
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("err = %v, want ErrAPIKeyNotFound", err)
	}
}

func TestLookupAPIKeyByHashIgnoresRevokedKey(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := testAPIKey("revoked key", "elevated")
	if err := store.InsertAPIKey(ctx, key); err != nil {
		t.Fatalf("InsertAPIKey: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE api_keys SET revoked_at = now() WHERE id = $1`, key.ID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}

	_, err := store.LookupAPIKeyByHash(ctx, key.KeyHash)
	if !errors.Is(err, ErrAPIKeyNotFound) {
		t.Fatalf("err = %v, want ErrAPIKeyNotFound for a revoked key", err)
	}
}
