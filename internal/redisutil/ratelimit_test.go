package redisutil

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestClientWithServer returns a Client plus the underlying miniredis
// server pinned to a fixed, known time via SetTime, so refill tests can
// advance it deterministically instead of racing a real sleep against the
// configured refill rate.
func newTestClientWithServer(t *testing.T) (*Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	mr.SetTime(time.Now())
	return New(&redis.Options{Addr: mr.Addr()}), mr
}

func TestAllowTokenBucketAllowsUpToCapacityThenDenies(t *testing.T) {
	c, _ := newTestClientWithServer(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		res, err := c.AllowTokenBucket(ctx, "ratelimit:test", 3, 1)
		if err != nil {
			t.Fatalf("AllowTokenBucket call %d: %v", i, err)
		}
		if !res.Allowed {
			t.Fatalf("call %d: Allowed = false, want true (remaining=%d)", i, res.Remaining)
		}
	}

	res, err := c.AllowTokenBucket(ctx, "ratelimit:test", 3, 1)
	if err != nil {
		t.Fatalf("AllowTokenBucket: %v", err)
	}
	if res.Allowed {
		t.Fatal("Allowed = true after exhausting capacity, want false")
	}
	if res.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %v, want > 0 when denied", res.RetryAfter)
	}
}

func TestAllowTokenBucketRefillsOverTime(t *testing.T) {
	c, mr := newTestClientWithServer(t)
	ctx := context.Background()
	start := time.Now()
	mr.SetTime(start)

	if res, err := c.AllowTokenBucket(ctx, "ratelimit:refill", 1, 1); err != nil || !res.Allowed {
		t.Fatalf("first call: allowed=%v err=%v, want allowed=true", res.Allowed, err)
	}
	if res, err := c.AllowTokenBucket(ctx, "ratelimit:refill", 1, 1); err != nil || res.Allowed {
		t.Fatalf("second call: allowed=%v err=%v, want allowed=false (bucket just drained)", res.Allowed, err)
	}

	mr.SetTime(start.Add(2 * time.Second))

	res, err := c.AllowTokenBucket(ctx, "ratelimit:refill", 1, 1)
	if err != nil {
		t.Fatalf("third call: %v", err)
	}
	if !res.Allowed {
		t.Fatal("third call: Allowed = false after 2s at refillPerSec=1, want true")
	}
}

func TestAllowTokenBucketDistinctKeysDoNotShareState(t *testing.T) {
	c, _ := newTestClientWithServer(t)
	ctx := context.Background()

	if res, err := c.AllowTokenBucket(ctx, "ratelimit:a", 1, 1); err != nil || !res.Allowed {
		t.Fatalf("key a: allowed=%v err=%v, want true", res.Allowed, err)
	}
	if res, err := c.AllowTokenBucket(ctx, "ratelimit:b", 1, 1); err != nil || !res.Allowed {
		t.Fatalf("key b: allowed=%v err=%v, want true (independent bucket)", res.Allowed, err)
	}
}
