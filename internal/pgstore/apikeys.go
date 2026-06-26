package pgstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrAPIKeyNotFound is returned by LookupAPIKeyByHash when no row matches
// the given hash, or the matching row has been revoked — callers don't
// need to distinguish "never existed" from "revoked", both mean reject the
// request.
var ErrAPIKeyNotFound = errors.New("pgstore: api key not found")

// APIKey is the durable record for a public API v1 elevated-tier
// credential, per docs/architecture/data-model.md. KeyHash is the SHA-256
// hex digest of the raw key handed to the caller at issuance time; the raw
// key itself is never stored.
type APIKey struct {
	ID        string
	KeyHash   string
	Label     string
	Tier      string
	CreatedAt time.Time
	RevokedAt *time.Time
}

// InsertAPIKey persists key, the write path for cmd/apigateway's
// -issue-key admin flag.
func (s *Store) InsertAPIKey(ctx context.Context, key APIKey) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO api_keys (id, key_hash, label, tier, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, key.ID, key.KeyHash, key.Label, key.Tier, key.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("pgstore: insert api key %s: %w", key.ID, err)
	}
	return nil
}

// LookupAPIKeyByHash returns the non-revoked APIKey whose KeyHash matches
// hash, or ErrAPIKeyNotFound if none exists. Called on the request path
// for every request bearing an API key, so the auth middleware can decide
// anonymous vs. elevated rate limits.
func (s *Store) LookupAPIKeyByHash(ctx context.Context, hash string) (APIKey, error) {
	var key APIKey
	err := s.pool.QueryRow(ctx, `
		SELECT id, key_hash, label, tier, created_at, revoked_at
		FROM api_keys
		WHERE key_hash = $1 AND revoked_at IS NULL
	`, hash).Scan(&key.ID, &key.KeyHash, &key.Label, &key.Tier, &key.CreatedAt, &key.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return APIKey{}, ErrAPIKeyNotFound
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("pgstore: lookup api key: %w", err)
	}
	return key, nil
}
