package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/health"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
	"github.com/alicebob/miniredis/v2"
	"github.com/nats-io/nats-server/v2/server"
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

func testRedisClientWithMiniredis(t *testing.T) (*redisutil.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return redisutil.New(&redis.Options{Addr: mr.Addr()}), mr
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testPublisher boots an in-process NATS server with JetStream enabled,
// ensures the flights.updates stream exists, and returns a publisher plus
// a subscriber for asserting what runMergeLoop actually published.
func testPublisher(t *testing.T) (*natsutil.FlightStatePublisher, *natsutil.FlightStateSubscriber) {
	t.Helper()

	srv, err := server.NewServer(&server.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("server.NewServer: %v", err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats test server: not ready for connections")
	}

	nc, err := natsutil.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("natsutil.Connect: %v", err)
	}
	t.Cleanup(nc.Close)

	js, err := natsutil.JetStream(nc)
	if err != nil {
		t.Fatalf("natsutil.JetStream: %v", err)
	}

	ctx := context.Background()
	if _, err := natsutil.EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}

	sub, err := natsutil.NewFlightStateSubscriber(ctx, js, "test-consumer")
	if err != nil {
		t.Fatalf("NewFlightStateSubscriber: %v", err)
	}

	return natsutil.NewFlightStatePublisher(js), sub
}

func TestRunMergeLoopWritesFlightStateToRedis(t *testing.T) {
	redisClient := testRedisClient(t)
	publisher, _ := testPublisher(t)
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

	runMergeLoop(ctx, testLogger(), redisClient, publisher, time.Hour, time.Minute)

	got, err := redisClient.ReadFlightState(context.Background(), "a1b2c3")
	if err != nil {
		t.Fatalf("ReadFlightState: %v", err)
	}
	if got.Callsign == nil || *got.Callsign != "UAL123" {
		t.Errorf("Callsign = %v, want UAL123", got.Callsign)
	}
}

func TestRunMergeLoopPublishesFlightStateToFlightsUpdates(t *testing.T) {
	redisClient := testRedisClient(t)
	publisher, sub := testPublisher(t)
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

	ctx, cancel := context.WithTimeout(ctx0, 20*time.Millisecond)
	defer cancel()
	runMergeLoop(ctx, testLogger(), redisClient, publisher, time.Hour, time.Minute)

	var got *flightmodel.FlightState
	done := make(chan struct{})
	errCh := make(chan error, 1)
	runCtx, runCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer runCancel()

	go func() {
		errCh <- sub.Run(runCtx, nil, func(state flightmodel.FlightState, _ time.Time) {
			if got == nil {
				got = &state
				close(done)
			}
		})
	}()

	select {
	case <-done:
	case err := <-errCh:
		t.Fatalf("sub.Run: %v", err)
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for flights.updates message")
	}

	if got.ICAO24 != "a1b2c3" {
		t.Errorf("ICAO24 = %q, want a1b2c3", got.ICAO24)
	}
	if got.Callsign == nil || *got.Callsign != "UAL123" {
		t.Errorf("Callsign = %v, want UAL123", got.Callsign)
	}
}

func TestPersistAndPublishSkipsPublishWhenWriteFails(t *testing.T) {
	redisClient := testRedisClient(t)
	publisher, sub := testPublisher(t)
	ctx := context.Background()

	if err := redisClient.Close(); err != nil {
		t.Fatalf("redisClient.Close: %v", err)
	}

	state := flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: time.Now().UTC()}
	persistAndPublish(ctx, testLogger(), redisClient, publisher, state, time.Minute, true)

	runCtx, runCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer runCancel()
	received := make(chan struct{})

	go func() {
		_ = sub.Run(runCtx, nil, func(flightmodel.FlightState, time.Time) {
			close(received)
		})
	}()

	select {
	case <-received:
		t.Fatal("PublishFlightState was called despite a failed Redis write")
	case <-runCtx.Done():
	}
}

// TestPersistAndPublishAllWritesAndPublishesEveryState exercises
// persistAndPublishAll's bounded worker pool with a state count well above
// mergeConcurrency, asserting every state is both persisted to Redis and
// published to flights.updates despite running concurrently rather than one
// at a time (see the merge-cycle-duration finding in
// docs/runbooks/load-test.md).
func TestPersistAndPublishAllWritesAndPublishesEveryState(t *testing.T) {
	redisClient := testRedisClient(t)
	publisher, sub := testPublisher(t)
	ctx := context.Background()

	const stateCount = mergeConcurrency * 3
	states := make([]flightmodel.FlightState, stateCount)
	now := time.Now().UTC()
	for i := range states {
		states[i] = flightmodel.FlightState{
			ICAO24:      icao24ForIndex(i),
			Lat:         1.0,
			Lon:         2.0,
			LastSeenUTC: now,
		}
	}

	received := make(map[string]struct{}, stateCount)
	var mu sync.Mutex
	done := make(chan struct{})
	runCtx, runCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer runCancel()

	go func() {
		_ = sub.Run(runCtx, nil, func(state flightmodel.FlightState, _ time.Time) {
			mu.Lock()
			received[state.ICAO24] = struct{}{}
			n := len(received)
			mu.Unlock()
			if n == stateCount {
				close(done)
			}
		})
	}()

	persistAndPublishAll(ctx, testLogger(), redisClient, publisher, states, time.Minute, map[string]time.Time{})

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatalf("timed out: received %d of %d published states", len(received), stateCount)
	}

	for _, state := range states {
		if _, err := redisClient.ReadFlightState(context.Background(), state.ICAO24); err != nil {
			t.Errorf("ReadFlightState(%s): %v", state.ICAO24, err)
		}
	}
}

func icao24ForIndex(i int) string {
	return fmt.Sprintf("a%05x", i)
}

// TestPersistAndPublishAllSkipsUnchangedState exercises the dedup fix from
// docs/runbooks/load-test.md's bottleneck findings: a state whose
// LastSeenUTC matches what was already published for that icao24 must be
// written to Redis (to keep its hot-state TTL alive) but not re-published,
// since flights.updates carrying the same stale LastSeenUTC on every merge
// cycle both wastes NATS throughput and corrupts the freshness SLO
// measurement.
func TestPersistAndPublishAllSkipsUnchangedState(t *testing.T) {
	redisClient, mr := testRedisClientWithMiniredis(t)
	publisher, sub := testPublisher(t)
	ctx := context.Background()
	const ttl = 200 * time.Millisecond

	state := flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 1.0, Lon: 2.0, LastSeenUTC: time.Now().UTC()}

	received := make(chan flightmodel.FlightState, 4)
	runCtx, runCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer runCancel()
	go func() {
		_ = sub.Run(runCtx, nil, func(s flightmodel.FlightState, _ time.Time) {
			received <- s
		})
	}()

	next := persistAndPublishAll(ctx, testLogger(), redisClient, publisher, []flightmodel.FlightState{state}, ttl, map[string]time.Time{})

	select {
	case <-received:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for first publish")
	}

	// Advance past half the TTL, then run a second cycle with the same
	// icao24 and same LastSeenUTC (no fresh raw report arrived) — must not
	// produce a second flights.updates message, but must still refresh the
	// Redis TTL since the hot state's underlying raw report is still live.
	mr.FastForward(ttl / 2)
	persistAndPublishAll(ctx, testLogger(), redisClient, publisher, []flightmodel.FlightState{state}, ttl, next)

	select {
	case s := <-received:
		t.Fatalf("unexpected republish of unchanged state: %+v", s)
	case <-time.After(300 * time.Millisecond):
	}

	// Advance past the remainder of the original TTL window. If the
	// skipped cycle above had not refreshed the TTL, the key would have
	// expired by now (it's already past the original ttl from the first
	// write); it only survives because the second cycle's write reset the
	// clock.
	mr.FastForward(ttl/2 + 50*time.Millisecond)
	if _, err := redisClient.ReadFlightState(context.Background(), state.ICAO24); err != nil {
		t.Errorf("ReadFlightState: %v, want hot state TTL refreshed on the skipped-publish cycle", err)
	}
}

func TestRunMergeLoopWritesNothingWhenNoRawState(t *testing.T) {
	redisClient := testRedisClient(t)
	publisher, _ := testPublisher(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	runMergeLoop(ctx, testLogger(), redisClient, publisher, time.Hour, time.Minute)

	if _, err := redisClient.ReadFlightState(context.Background(), "a1b2c3"); !errors.Is(err, redis.Nil) {
		t.Fatalf("ReadFlightState: err = %v, want redis.Nil", err)
	}
}

func TestRunMergeLoopPublishesOnceAcrossMultipleCyclesOfUnchangedRawState(t *testing.T) {
	redisClient := testRedisClient(t)
	publisher, sub := testPublisher(t)
	ctx0 := context.Background()

	raw := sourceadapter.RawState{
		Provider:  "airplanes.live",
		ICAO24:    "a1b2c3",
		FetchedAt: time.Now().UTC(),
		Payload:   json.RawMessage(`{"hex":"a1b2c3","flight":"UAL123","lat":37.0,"lon":-122.0,"alt_baro":35000,"type":"adsb_icao"}`),
	}
	if err := redisClient.WriteRawState(ctx0, raw, time.Minute); err != nil {
		t.Fatalf("WriteRawState: %v", err)
	}

	// A short interval over a longer-lived ctx runs several merge cycles
	// against the same never-rewritten raw state, mirroring the
	// raw-state-ttl-outlives-merge-interval scenario the load test
	// (docs/runbooks/load-test.md) exercised at scale.
	ctx, cancel := context.WithTimeout(ctx0, 120*time.Millisecond)
	defer cancel()

	received := make(chan struct{}, 8)
	runCtx, runCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer runCancel()
	go func() {
		_ = sub.Run(runCtx, nil, func(flightmodel.FlightState, time.Time) {
			received <- struct{}{}
		})
	}()

	runMergeLoop(ctx, testLogger(), redisClient, publisher, 20*time.Millisecond, time.Minute)

	deadline := time.After(300 * time.Millisecond)
	count := 0
loop:
	for {
		select {
		case <-received:
			count++
		case <-deadline:
			break loop
		}
	}

	if count != 1 {
		t.Errorf("flights.updates message count = %d, want exactly 1 across multiple merge cycles of unchanged raw state", count)
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
