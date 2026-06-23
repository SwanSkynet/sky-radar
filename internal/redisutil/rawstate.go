package redisutil

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
)

// RawStateKey returns the Redis key for one provider/aircraft raw payload.
//
// This key is a Phase 1 interim wire-through, not part of the canonical
// schema in docs/architecture/data-model.md: until NATS JetStream lands in
// Phase 2, adapters write straight to Redis instead of publishing to
// ingest.raw.<provider> (see docs/implementation-plan.md, "One source
// adapter end-to-end").
func RawStateKey(provider, icao24 string) string {
	return fmt.Sprintf("raw:%s:%s", provider, icao24)
}

// WriteRawState stores state under RawStateKey, overwriting any previous
// payload for that provider/aircraft pair, expiring after ttl.
func (c *Client) WriteRawState(ctx context.Context, state sourceadapter.RawState, ttl time.Duration) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("redisutil: marshal raw state: %w", err)
	}

	key := RawStateKey(state.Provider, state.ICAO24)
	if err := c.rdb.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("redisutil: write raw state %s: %w", key, err)
	}
	return nil
}

// ReadRawState fetches and decodes the raw payload previously written by
// WriteRawState, returning redis.Nil (via errors.Is) if it has expired or
// was never written.
func (c *Client) ReadRawState(ctx context.Context, provider, icao24 string) (sourceadapter.RawState, error) {
	var state sourceadapter.RawState

	data, err := c.rdb.Get(ctx, RawStateKey(provider, icao24)).Bytes()
	if err != nil {
		return state, fmt.Errorf("redisutil: read raw state: %w", err)
	}

	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("redisutil: unmarshal raw state: %w", err)
	}
	return state, nil
}
