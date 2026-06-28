package redisutil

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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
	if strings.TrimSpace(state.Provider) == "" || strings.TrimSpace(state.ICAO24) == "" {
		return fmt.Errorf("redisutil: write raw state: provider and icao24 are required")
	}

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

// WriteRawStatesConcurrently calls WriteRawState for every entry in states,
// bounded by concurrency in-flight writes at once, so writing a full poll
// batch costs approximately one Redis round trip rather than len(states) of
// them run back to back. Every adapter (cmd/adapter-opensky,
// cmd/adapter-adsblol, cmd/adapter-airplaneslive) and the ingest-volume load
// test wrote their poll batches with a plain sequential for-loop until the
// load test in docs/runbooks/load-test.md showed that loop's wall-clock cost
// growing linearly with batch size eating into the per-aircraft freshness
// budget as tracked-aircraft count rose toward the master PRD's 50,000
// headroom target — the same scaling problem persistAndPublishAll already
// solved on the normalizer's publish side. onError, if non-nil, is called
// for each write that fails; it must be safe to call concurrently.
func (c *Client) WriteRawStatesConcurrently(ctx context.Context, states []sourceadapter.RawState, ttl time.Duration, concurrency int, onError func(sourceadapter.RawState, error)) {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, state := range states {
		wg.Add(1)
		sem <- struct{}{}
		go func(state sourceadapter.RawState) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := c.WriteRawState(ctx, state, ttl); err != nil && onError != nil {
				onError(state, err)
			}
		}(state)
	}
	wg.Wait()
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

// ScanRawStates returns every RawState currently written under the raw:*
// keyspace, across every provider and aircraft. The normalizer's merge
// loop uses this to discover what's currently being reported (see
// docs/tech-stack/backend.md); an entry that fails to decode is skipped
// rather than failing the whole scan, mirroring MergeAll's per-aircraft
// fault isolation.
//
// Keys are collected via SCAN and then fetched in a single MGET rather
// than one GET per key, so a keyspace of N raw:* entries costs O(1) round
// trips instead of O(N) — this runs every merge interval, so the round-trip
// count matters as the tracked-aircraft count grows.
func (c *Client) ScanRawStates(ctx context.Context) ([]sourceadapter.RawState, error) {
	var keys []string
	iter := c.rdb.Scan(ctx, 0, "raw:*", 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redisutil: scan raw states: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}

	values, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("redisutil: scan raw states: %w", err)
	}

	states := make([]sourceadapter.RawState, 0, len(values))
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			// A key expired between SCAN and MGET; skip it rather than
			// failing the whole batch.
			continue
		}
		var state sourceadapter.RawState
		if err := json.Unmarshal([]byte(s), &state); err != nil {
			continue
		}
		states = append(states, state)
	}
	return states, nil
}
