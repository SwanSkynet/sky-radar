package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestHealthzOKWhenRedisReachable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(testRedisClient(t)))

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

func TestHealthzUnavailableWhenRedisUnreachable(t *testing.T) {
	redisClient := redisutil.New(&redis.Options{Addr: "127.0.0.1:0"})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(redisClient))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
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
		{Provider: providerName, ICAO24: "abc123", FetchedAt: time.Now(), Payload: json.RawMessage(`["abc123"]`)},
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
		{Provider: providerName, ICAO24: "abc123", Payload: json.RawMessage(`["abc123"]`)},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	runPollLoop(ctx, testLogger(), adapter, redisClient, time.Millisecond, time.Minute)

	if calls := atomic.LoadInt32(&adapter.calls); calls < 2 {
		t.Fatalf("Poll called %d times, want at least 2 (loop should repeat on the ticker)", calls)
	}
}

func TestEnvHelpersFallBackWhenUnset(t *testing.T) {
	const key = "ADAPTER_OPENSKY_TEST_UNSET_KEY"
	t.Setenv(key, "")

	if got := envString(key, "fallback"); got != "fallback" {
		t.Errorf("envString = %q, want fallback", got)
	}
	if got := envDuration(key, 9*time.Second); got != 9*time.Second {
		t.Errorf("envDuration = %v, want 9s", got)
	}
}

func TestEnvHelpersReadConfiguredValues(t *testing.T) {
	const key = "ADAPTER_OPENSKY_TEST_SET_KEY"
	t.Setenv(key, "12")

	if got := envDuration(key, 0); got != 12*time.Second {
		t.Errorf("envDuration = %v, want 12s", got)
	}
}

func TestEnvBoundingBoxRequiresAllFourCoords(t *testing.T) {
	for _, key := range []string{"OPENSKY_LAMIN", "OPENSKY_LOMIN", "OPENSKY_LAMAX", "OPENSKY_LOMAX"} {
		t.Setenv(key, "")
	}

	t.Setenv("OPENSKY_LAMIN", "45.8389")
	t.Setenv("OPENSKY_LOMIN", "5.9962")
	t.Setenv("OPENSKY_LAMAX", "47.8229")
	// OPENSKY_LOMAX intentionally left unset.

	if got := envBoundingBox(); got != nil {
		t.Errorf("envBoundingBox() = %+v, want nil when one coord is missing", got)
	}

	t.Setenv("OPENSKY_LOMAX", "10.5226")
	got := envBoundingBox()
	if got == nil {
		t.Fatal("envBoundingBox() = nil, want a BoundingBox when all four coords are set")
	}
	want := BoundingBox{LaMin: 45.8389, LoMin: 5.9962, LaMax: 47.8229, LoMax: 10.5226}
	if *got != want {
		t.Errorf("envBoundingBox() = %+v, want %+v", *got, want)
	}
}
