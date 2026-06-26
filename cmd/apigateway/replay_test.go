package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
)

// replayTestDSN mirrors internal/pgstore's testdb_test.go default: the
// postgres service container CI and docker-compose both run, overridable
// via TEST_DATABASE_URL for any other environment.
const replayTestDSN = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

func testReplayPostgres(t *testing.T) *pgstore.Store {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = replayTestDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store, err := pgstore.Connect(ctx, dsn)
	if err != nil {
		t.Skipf("postgres unavailable, skipping integration test: %v", err)
	}
	t.Cleanup(store.Close)

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

func testReplayJetStream(t *testing.T) (jetstream.JetStream, *natsutil.FlightStatePublisher) {
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
	if _, err := natsutil.EnsureFlightsUpdatesStream(context.Background(), js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}

	return js, natsutil.NewFlightStatePublisher(js)
}

// testReplayAPI wires a replayAPI against a real in-process NATS/JetStream
// server and a real Postgres connection (skipping the test if Postgres
// isn't reachable, same convention as internal/pgstore's own tests),
// because the replay window logic genuinely spans both stores and a fake
// for either would just be re-testing this package's own merge logic
// against a mock instead of the real data sources.
func testReplayAPI(t *testing.T) (api *replayAPI, pub *natsutil.FlightStatePublisher, pg *pgstore.Store) {
	t.Helper()
	js, pub := testReplayJetStream(t)
	pg = testReplayPostgres(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &replayAPI{js: js, pg: pg, logger: logger}, pub, pg
}

func doGetReplay(t *testing.T, api *replayAPI, query string) *httptest.ResponseRecorder {
	t.Helper()
	mux := newRouter(&flightsAPI{logger: api.logger}, nil, api)
	req := httptest.NewRequest(http.MethodGet, "/replay"+query, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeReplayBody(t *testing.T, rec *httptest.ResponseRecorder) []pgstore.FlightHistoryRecord {
	t.Helper()
	var got []pgstore.FlightHistoryRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal replay response: %v (body: %s)", err, rec.Body.String())
	}
	return got
}

func TestGetReplayRequiresFromAndTo(t *testing.T) {
	api, _, _ := testReplayAPI(t)

	rec := doGetReplay(t, api, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetReplayRejectsInvalidTimestamps(t *testing.T) {
	api, _, _ := testReplayAPI(t)

	rec := doGetReplay(t, api, "?from=not-a-time&to=2026-01-01T00:00:00Z")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetReplayRejectsToBeforeFrom(t *testing.T) {
	api, _, _ := testReplayAPI(t)

	now := time.Now().UTC()
	from := now.Format(time.RFC3339)
	to := now.Add(-time.Minute).Format(time.RFC3339)
	rec := doGetReplay(t, api, fmt.Sprintf("?from=%s&to=%s", from, to))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetReplayRejectsWindowExceedingMax(t *testing.T) {
	api, _, _ := testReplayAPI(t)

	origMax := replayMaxWindow
	replayMaxWindow = time.Minute
	t.Cleanup(func() { replayMaxWindow = origMax })

	now := time.Now().UTC()
	from := now.Add(-10 * time.Minute).Format(time.RFC3339)
	to := now.Format(time.RFC3339)
	rec := doGetReplay(t, api, fmt.Sprintf("?from=%s&to=%s", from, to))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetReplayReturnsJetStreamSamplesWithinWindow(t *testing.T) {
	api, pub, _ := testReplayAPI(t)
	ctx := context.Background()
	icao24 := fmt.Sprintf("r%d", time.Now().UnixNano())

	if err := pub.PublishFlightState(ctx, flightmodel.FlightState{ICAO24: icao24, Lat: 10, Lon: 20, LastSeenUTC: time.Now().UTC()}); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}

	now := time.Now().UTC()
	from := now.Add(-time.Minute).Format(time.RFC3339)
	to := now.Add(time.Minute).Format(time.RFC3339)
	rec := doGetReplay(t, api, fmt.Sprintf("?from=%s&to=%s", from, to))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var forAircraft []pgstore.FlightHistoryRecord
	for _, s := range decodeReplayBody(t, rec) {
		if s.ICAO24 == icao24 {
			forAircraft = append(forAircraft, s)
		}
	}
	if len(forAircraft) != 1 {
		t.Fatalf("got %d samples for %s, want 1: %+v", len(forAircraft), icao24, forAircraft)
	}
	if forAircraft[0].Lat != 10 || forAircraft[0].Lon != 20 {
		t.Errorf("lat/lon = %v/%v, want 10/20", forAircraft[0].Lat, forAircraft[0].Lon)
	}
}

func TestGetReplayFiltersByBBox(t *testing.T) {
	api, pub, _ := testReplayAPI(t)
	ctx := context.Background()
	base := time.Now().UnixNano()
	inside := fmt.Sprintf("i%d", base)
	outside := fmt.Sprintf("o%d", base)

	if err := pub.PublishFlightState(ctx, flightmodel.FlightState{ICAO24: inside, Lat: 37.0, Lon: -122.0, LastSeenUTC: time.Now().UTC()}); err != nil {
		t.Fatalf("PublishFlightState(inside): %v", err)
	}
	if err := pub.PublishFlightState(ctx, flightmodel.FlightState{ICAO24: outside, Lat: 50.0, Lon: -100.0, LastSeenUTC: time.Now().UTC()}); err != nil {
		t.Fatalf("PublishFlightState(outside): %v", err)
	}

	now := time.Now().UTC()
	from := now.Add(-time.Minute).Format(time.RFC3339)
	to := now.Add(time.Minute).Format(time.RFC3339)
	rec := doGetReplay(t, api, fmt.Sprintf("?from=%s&to=%s&bbox=-123,36,-121,38", from, to))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	got := decodeReplayBody(t, rec)
	foundInside := false
	for _, s := range got {
		if s.ICAO24 == outside {
			t.Fatalf("out-of-bbox aircraft %s present in replay response: %+v", outside, s)
		}
		if s.ICAO24 == inside {
			foundInside = true
		}
	}
	if !foundInside {
		t.Fatalf("in-bbox aircraft %s missing from replay response: %+v", inside, got)
	}
}

// TestGetReplayCombinesPostgresHistoryForOlderWindow seeds a Postgres
// flight_history row older than natsutil.FlightsUpdatesMaxAge (so it's
// past JetStream's retention) alongside a JetStream message published
// "now", then asserts a window spanning both returns both, with the
// Postgres-backed (older) sample ordered first — without needing to
// actually wait out a real 30-minute retention window.
func TestGetReplayCombinesPostgresHistoryForOlderWindow(t *testing.T) {
	api, pub, pg := testReplayAPI(t)
	ctx := context.Background()
	icao24Old := fmt.Sprintf("h%d", time.Now().UnixNano())
	icao24New := fmt.Sprintf("n%d", time.Now().UnixNano())

	now := time.Now().UTC()
	oldRecordedAt := now.Add(-40 * time.Minute).Truncate(time.Microsecond)
	if err := pg.InsertFlightHistory(ctx, pgstore.FlightHistoryRecord{
		ICAO24: icao24Old, RecordedAt: oldRecordedAt, Lat: 1, Lon: 2,
	}); err != nil {
		t.Fatalf("InsertFlightHistory: %v", err)
	}

	if err := pub.PublishFlightState(ctx, flightmodel.FlightState{ICAO24: icao24New, Lat: 3, Lon: 4, LastSeenUTC: now}); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}

	from := now.Add(-45 * time.Minute).Format(time.RFC3339)
	to := now.Add(time.Minute).Format(time.RFC3339)
	rec := doGetReplay(t, api, fmt.Sprintf("?from=%s&to=%s", from, to))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	got := decodeReplayBody(t, rec)
	oldIdx, newIdx := -1, -1
	for i, s := range got {
		switch s.ICAO24 {
		case icao24Old:
			oldIdx = i
		case icao24New:
			newIdx = i
		}
	}
	if oldIdx == -1 {
		t.Fatalf("postgres-backed sample %s missing from replay response: %+v", icao24Old, got)
	}
	if newIdx == -1 {
		t.Fatalf("jetstream-backed sample %s missing from replay response: %+v", icao24New, got)
	}
	if oldIdx > newIdx {
		t.Errorf("postgres sample (idx %d) should be ordered before jetstream sample (idx %d)", oldIdx, newIdx)
	}
}
