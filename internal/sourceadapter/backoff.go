package sourceadapter

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// Backoff computes exponential backoff durations with jitter, used by
// adapters to back off on 429/5xx responses per their provider's
// documented rate limits (see docs/tech-stack/backend.md).
type Backoff struct {
	Base    time.Duration
	Max     time.Duration
	attempt int
}

// NewBackoff returns a Backoff starting at base and capped at max.
func NewBackoff(base, max time.Duration) *Backoff {
	return &Backoff{Base: base, Max: max}
}

// Next returns the delay before the next retry attempt and advances the
// internal attempt counter. The delay is half the exponential value plus
// up to half again in jitter, capped at Max.
func (b *Backoff) Next() time.Duration {
	d := b.Base
	for i := 0; i < b.attempt && d < b.Max; i++ {
		d *= 2
	}
	if d > b.Max || d <= 0 {
		d = b.Max
	}
	b.attempt++

	half := int64(d) / 2
	jitter := time.Duration(0)
	if half > 0 {
		jitter = time.Duration(rand.Int63n(half))
	}
	return d/2 + jitter
}

// Reset clears the attempt counter, e.g. after a successful call.
func (b *Backoff) Reset() {
	b.attempt = 0
}

// RetryableError marks an error as eligible for backoff-and-retry, e.g. an
// HTTP 429/5xx response. RetryAfter, if non-zero, overrides the computed
// backoff delay (e.g. from a provider's Retry-After header).
type RetryableError struct {
	Err        error
	RetryAfter time.Duration
}

func (e *RetryableError) Error() string { return e.Err.Error() }
func (e *RetryableError) Unwrap() error { return e.Err }

// Retry calls fn until it succeeds, returns a non-retryable error, ctx is
// done, or maxAttempts is exhausted — backing off between attempts per b.
func Retry(ctx context.Context, b *Backoff, maxAttempts int, fn func() error) error {
	if maxAttempts <= 0 {
		return fmt.Errorf("sourceadapter: maxAttempts must be > 0, got %d", maxAttempts)
	}

	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			b.Reset()
			return nil
		}

		var retryable *RetryableError
		if !errors.As(err, &retryable) {
			return err
		}

		if attempt == maxAttempts-1 {
			break
		}

		wait := b.Next()
		if retryable.RetryAfter > 0 {
			wait = retryable.RetryAfter
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return err
}
