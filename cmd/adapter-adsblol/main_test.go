package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestHealthz(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
}

type fakeAdapter struct {
	states []sourceadapter.RawState
	err    error
	calls  int32
}

func (f *fakeAdapter) Poll(ctx context.Context) ([]sourceadapter.RawState, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.err != nil {
		return nil, f.err
	}
	return f.states, nil
}

func testRedisClient(t *testing.T) *redisutil.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redisutil.New(&redis.Options{Addr: mr.Addr()})
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunPollLoopWritesRawStateToRedis(t *testing.T) {
	redisClient := testRedisClient(t)
	adapter := &fakeAdapter{states: []sourceadapter.RawState{
		{Provider: providerName, ICAO24: "abc123", FetchedAt: time.Now(), Payload: json.RawMessage(`{"hex":"abc123"}`)},
	}}

	// A huge interval means the loop blocks in select{} after its first
	// Poll+write, so the short timeout below deterministically stops it
	// after exactly one iteration once that first write has gone through.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	runPollLoop(ctx, testLogger(), adapter, redisClient, time.Hour, time.Minute)

	got, err := redisClient.ReadRawState(context.Background(), providerName, "abc123")
	if err != nil {
		t.Fatalf("ReadRawState: %v", err)
	}
	if got.ICAO24 != "abc123" {
		t.Errorf("ICAO24 = %q, want abc123", got.ICAO24)
	}
}

func TestRunPollLoopSkipsRedisWriteOnPollError(t *testing.T) {
	redisClient := testRedisClient(t)
	adapter := &fakeAdapter{err: errors.New("provider unavailable")}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	runPollLoop(ctx, testLogger(), adapter, redisClient, time.Hour, time.Minute)

	if _, err := redisClient.ReadRawState(context.Background(), providerName, "abc123"); !errors.Is(err, redis.Nil) {
		t.Fatalf("ReadRawState: err = %v, want redis.Nil (nothing should be written on poll error)", err)
	}
}

func TestRunPollLoopContinuesPollingAcrossTicks(t *testing.T) {
	redisClient := testRedisClient(t)
	adapter := &fakeAdapter{states: []sourceadapter.RawState{
		{Provider: providerName, ICAO24: "abc123", Payload: json.RawMessage(`{"hex":"abc123"}`)},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	runPollLoop(ctx, testLogger(), adapter, redisClient, time.Millisecond, time.Minute)

	if calls := atomic.LoadInt32(&adapter.calls); calls < 2 {
		t.Fatalf("Poll called %d times, want at least 2 (loop should repeat on the ticker)", calls)
	}
}

func TestEnvHelpersFallBackWhenUnset(t *testing.T) {
	const key = "ADAPTER_ADSBLOL_TEST_UNSET_KEY"
	os.Unsetenv(key)

	if got := envString(key, "fallback"); got != "fallback" {
		t.Errorf("envString = %q, want fallback", got)
	}
	if got := envFloat(key, 1.5); got != 1.5 {
		t.Errorf("envFloat = %v, want 1.5", got)
	}
	if got := envInt(key, 7); got != 7 {
		t.Errorf("envInt = %v, want 7", got)
	}
	if got := envDuration(key, 9*time.Second); got != 9*time.Second {
		t.Errorf("envDuration = %v, want 9s", got)
	}
}

func TestEnvHelpersReadConfiguredValues(t *testing.T) {
	const key = "ADAPTER_ADSBLOL_TEST_SET_KEY"
	t.Setenv(key, "12")

	if got := envInt(key, 0); got != 12 {
		t.Errorf("envInt = %v, want 12", got)
	}
	if got := envFloat(key, 0); got != 12 {
		t.Errorf("envFloat = %v, want 12", got)
	}
	if got := envDuration(key, 0); got != 12*time.Second {
		t.Errorf("envDuration = %v, want 12s", got)
	}
}
