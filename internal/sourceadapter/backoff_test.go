package sourceadapter

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBackoffNextCapsAtMax(t *testing.T) {
	b := NewBackoff(10*time.Millisecond, 80*time.Millisecond)
	for i := 0; i < 10; i++ {
		d := b.Next()
		if d > 80*time.Millisecond {
			t.Fatalf("attempt %d: delay %s exceeds max %s", i, d, 80*time.Millisecond)
		}
		if d < 0 {
			t.Fatalf("attempt %d: negative delay %s", i, d)
		}
	}
}

func TestBackoffResetRestartsGrowth(t *testing.T) {
	b := NewBackoff(10*time.Millisecond, time.Second)
	for i := 0; i < 5; i++ {
		b.Next()
	}
	b.Reset()
	if b.attempt != 0 {
		t.Fatalf("attempt = %d after Reset, want 0", b.attempt)
	}
}

func TestRetrySucceedsAfterRetryableErrors(t *testing.T) {
	b := NewBackoff(time.Millisecond, 5*time.Millisecond)
	calls := 0
	err := Retry(context.Background(), b, 5, func() error {
		calls++
		if calls < 3 {
			return &RetryableError{Err: errors.New("rate limited")}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Retry returned error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetryStopsOnNonRetryableError(t *testing.T) {
	b := NewBackoff(time.Millisecond, 5*time.Millisecond)
	wantErr := errors.New("permanent failure")
	calls := 0
	err := Retry(context.Background(), b, 5, func() error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on non-retryable error)", calls)
	}
}

func TestRetryExhaustsMaxAttempts(t *testing.T) {
	b := NewBackoff(time.Millisecond, 5*time.Millisecond)
	calls := 0
	err := Retry(context.Background(), b, 3, func() error {
		calls++
		return &RetryableError{Err: errors.New("rate limited")}
	})
	if err == nil {
		t.Fatal("Retry returned nil error, want exhaustion error")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRetryHonorsRetryAfter(t *testing.T) {
	b := NewBackoff(time.Hour, time.Hour) // would block forever without RetryAfter override
	calls := 0
	start := time.Now()
	err := Retry(context.Background(), b, 2, func() error {
		calls++
		if calls < 2 {
			return &RetryableError{Err: errors.New("rate limited"), RetryAfter: time.Millisecond}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Retry returned error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Retry took %s, RetryAfter override was not honored", elapsed)
	}
}

func TestRetryStopsOnContextCancellation(t *testing.T) {
	b := NewBackoff(time.Hour, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Retry(ctx, b, 5, func() error {
		return &RetryableError{Err: errors.New("rate limited")}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
