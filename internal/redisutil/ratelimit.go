package redisutil

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// rateLimitScript implements a token bucket entirely server-side in a
// single Lua script, so concurrent requests against the same key (from
// different apigateway instances, per docs/tech-stack/backend.md's
// "in Redis so it works across multiple gateway instances") can't race
// each other into double-spending tokens, and so the bucket's clock is
// Redis's own (via TIME) rather than each caller's possibly-skewed wall
// clock.
//
// KEYS[1] = bucket key
// ARGV[1] = capacity (max tokens / burst size)
// ARGV[2] = refill tokens per second
// ARGV[3] = TTL to set on the bucket key, in milliseconds (bounds memory
//
//	for buckets that go idle; a bucket missing entirely is treated
//	as full, so an expired key is harmless)
//
// Returns {allowed (0/1), tokens_remaining (floored), retry_after_ms}.
var rateLimitScript = redis.NewScript(`
local capacity = tonumber(ARGV[1])
local refill_per_sec = tonumber(ARGV[2])
local ttl_ms = tonumber(ARGV[3])

local bucket = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(bucket[1])
local ts = tonumber(bucket[2])

local time_parts = redis.call('TIME')
local now_ms = tonumber(time_parts[1]) * 1000 + math.floor(tonumber(time_parts[2]) / 1000)

if tokens == nil then
  tokens = capacity
  ts = now_ms
end

local elapsed_ms = now_ms - ts
if elapsed_ms < 0 then
  elapsed_ms = 0
end
tokens = math.min(capacity, tokens + (elapsed_ms / 1000.0) * refill_per_sec)

local allowed = 0
local retry_after_ms = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  retry_after_ms = math.ceil((1 - tokens) / refill_per_sec * 1000)
end

redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'ts', tostring(now_ms))
redis.call('PEXPIRE', KEYS[1], ttl_ms)

return {allowed, math.floor(tokens), retry_after_ms}
`)

// rateLimitBucketTTL bounds how long an idle bucket's Redis key survives.
// It must comfortably exceed capacity/refillPerSec (the time a fully
// drained bucket takes to refill) for any tier this is used with, or an
// abusive client could get a free refill by going quiet just past TTL and
// coming back to a reset bucket; the actual call sites here top out at a
// few minutes of refill time, so a generous fixed TTL is simpler than
// computing one per call.
const rateLimitBucketTTL = 10 * time.Minute

// TokenBucketResult is the outcome of a single AllowTokenBucket call.
type TokenBucketResult struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

// AllowTokenBucket atomically consumes one token from the bucket
// identified by key, refilling at refillPerSec tokens/sec up to capacity,
// and reports whether the request is allowed. A bucket that doesn't exist
// yet starts full (capacity tokens), so the first request against a new
// key is always allowed.
func (c *Client) AllowTokenBucket(ctx context.Context, key string, capacity int, refillPerSec float64) (TokenBucketResult, error) {
	if capacity <= 0 || refillPerSec <= 0 {
		return TokenBucketResult{}, fmt.Errorf("redisutil: token bucket %s: capacity (%d) and refillPerSec (%g) must both be positive", key, capacity, refillPerSec)
	}

	res, err := rateLimitScript.Run(ctx, c.rdb, []string{key}, capacity, refillPerSec, rateLimitBucketTTL.Milliseconds()).Slice()
	if err != nil {
		return TokenBucketResult{}, fmt.Errorf("redisutil: token bucket %s: %w", key, err)
	}
	if len(res) != 3 {
		return TokenBucketResult{}, fmt.Errorf("redisutil: token bucket %s: unexpected script result %v", key, res)
	}

	allowed, _ := res[0].(int64)
	remaining, _ := res[1].(int64)
	retryAfterMs, _ := res[2].(int64)

	return TokenBucketResult{
		Allowed:    allowed == 1,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retryAfterMs) * time.Millisecond,
	}, nil
}
