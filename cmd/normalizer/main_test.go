package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/health"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestHealthzRouteWiredToLive(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadyzUnavailableWhenRedisUnreachable(t *testing.T) {
	redisClient := redisutil.New(&redis.Options{Addr: "127.0.0.1:0"})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /readyz", health.Ready(redisClient))

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func testRedisClient(t *testing.T) *redisutil.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redisutil.New(&redis.Options{Addr: mr.Addr()})
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunMergeLoopWritesFlightStateToRedis(t *testing.T) {
	redisClient := testRedisClient(t)
	ctx0 := context.Background()

	now := time.Now().UTC()
	raw := sourceadapter.RawState{
		Provider:  "airplanes.live",
		ICAO24:    "a1b2c3",
		FetchedAt: now,
		Payload:   json.RawMessage(`{"hex":"a1b2c3","flight":"UAL123","lat":37.0,"lon":-122.0,"alt_baro":35000,"type":"adsb_icao"}`),
	}
	if err := redisClient.WriteRawState(ctx0, raw, time.Minute); err != nil {
		t.Fatalf("WriteRawState: %v", err)
	}

	// A huge interval means the loop blocks in select{} after its first
	// scan+merge+write, so the short timeout below deterministically stops
	// it after exactly one iteration once that first write has gone through.
	ctx, cancel := context.WithTimeout(ctx0, 20*time.Millisecond)
	defer cancel()

	runMergeLoop(ctx, testLogger(), redisClient, time.Hour, time.Minute)

	got, err := redisClient.ReadFlightState(context.Background(), "a1b2c3")
	if err != nil {
		t.Fatalf("ReadFlightState: %v", err)
	}
	if got.Callsign == nil || *got.Callsign != "UAL123" {
		t.Errorf("Callsign = %v, want UAL123", got.Callsign)
	}
}

func TestRunMergeLoopWritesNothingWhenNoRawState(t *testing.T) {
	redisClient := testRedisClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	runMergeLoop(ctx, testLogger(), redisClient, time.Hour, time.Minute)

	if _, err := redisClient.ReadFlightState(context.Background(), "a1b2c3"); !errors.Is(err, redis.Nil) {
		t.Fatalf("ReadFlightState: err = %v, want redis.Nil", err)
	}
}

func TestEnvHelpersFallBackWhenUnset(t *testing.T) {
	const key = "NORMALIZER_TEST_UNSET_KEY"
	t.Setenv(key, "")

	if got := envString(key, "fallback"); got != "fallback" {
		t.Errorf("envString = %q, want fallback", got)
	}
	if got := envDuration(key, 9*time.Second); got != 9*time.Second {
		t.Errorf("envDuration = %v, want 9s", got)
	}
}

func TestEnvHelpersReadConfiguredValues(t *testing.T) {
	const key = "NORMALIZER_TEST_SET_KEY"
	t.Setenv(key, "12")

	if got := envDuration(key, 0); got != 12*time.Second {
		t.Errorf("envDuration = %v, want 12s", got)
	}
}
