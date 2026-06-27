package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/nats-io/nats-server/v2/server"
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

func TestEnvStringFallsBackWhenUnset(t *testing.T) {
	const key = "EVENTENGINE_TEST_UNSET_KEY"
	t.Setenv(key, "")

	if got := envString(key, "fallback"); got != "fallback" {
		t.Errorf("envString = %q, want fallback", got)
	}
}

func TestEnvZonesUnsetReturnsNilNoError(t *testing.T) {
	const key = "EVENTENGINE_TEST_ZONES_UNSET"
	t.Setenv(key, "")

	zones, err := envZones(key)
	if err != nil {
		t.Fatalf("envZones: unexpected error %v", err)
	}
	if zones != nil {
		t.Errorf("zones = %v, want nil", zones)
	}
}

func TestEnvZonesValidJSONParses(t *testing.T) {
	const key = "EVENTENGINE_TEST_ZONES_VALID"
	t.Setenv(key, `[{"id":"z1","name":"Zone One","polygon":{"type":"Polygon","coordinates":[[[-122.5,37.5],[-122.0,37.5],[-122.0,38.0],[-122.5,38.0],[-122.5,37.5]]]}}]`)

	zones, err := envZones(key)
	if err != nil {
		t.Fatalf("envZones: unexpected error %v", err)
	}
	if len(zones) != 1 || zones[0].ID != "z1" {
		t.Errorf("zones = %+v, want a single zone z1", zones)
	}
}

func TestEnvZonesMalformedJSONReturnsError(t *testing.T) {
	const key = "EVENTENGINE_TEST_ZONES_MALFORMED"
	t.Setenv(key, `not valid json`)

	zones, err := envZones(key)
	if err == nil {
		t.Fatal("envZones: want error for malformed JSON, got nil")
	}
	if zones != nil {
		t.Errorf("zones = %v, want nil on error", zones)
	}
}

func TestEnvWatchlistEntriesUnsetReturnsNilNoError(t *testing.T) {
	const key = "EVENTENGINE_TEST_WATCHLIST_UNSET"
	t.Setenv(key, "")

	entries, err := envWatchlistEntries(key)
	if err != nil {
		t.Fatalf("envWatchlistEntries: unexpected error %v", err)
	}
	if entries != nil {
		t.Errorf("entries = %v, want nil", entries)
	}
}

func TestEnvWatchlistEntriesValidJSONParses(t *testing.T) {
	const key = "EVENTENGINE_TEST_WATCHLIST_VALID"
	t.Setenv(key, `[{"id":"w1","icao24":"a1b2c3","label":"Friend's flight"}]`)

	entries, err := envWatchlistEntries(key)
	if err != nil {
		t.Fatalf("envWatchlistEntries: unexpected error %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "w1" {
		t.Errorf("entries = %+v, want a single entry w1", entries)
	}
}

func TestEnvWatchlistEntriesMalformedJSONReturnsError(t *testing.T) {
	const key = "EVENTENGINE_TEST_WATCHLIST_MALFORMED"
	t.Setenv(key, `not valid json`)

	entries, err := envWatchlistEntries(key)
	if err == nil {
		t.Fatal("envWatchlistEntries: want error for malformed JSON, got nil")
	}
	if entries != nil {
		t.Errorf("entries = %v, want nil on error", entries)
	}
}

func TestMergeZonesOverlayWinsOnConflictingID(t *testing.T) {
	base := []flightmodel.Zone{{ID: "z1", Name: "Base Name"}, {ID: "z2", Name: "Base Only"}}
	overlay := []flightmodel.Zone{{ID: "z1", Name: "Overlay Name"}, {ID: "z3", Name: "Overlay Only"}}

	merged := mergeZones(base, overlay)

	byID := make(map[string]flightmodel.Zone, len(merged))
	for _, z := range merged {
		byID[z.ID] = z
	}
	if len(byID) != 3 {
		t.Fatalf("merged %d zones, want 3: %+v", len(byID), merged)
	}
	if byID["z1"].Name != "Overlay Name" {
		t.Errorf("z1.Name = %q, want overlay to win: %q", byID["z1"].Name, "Overlay Name")
	}
	if byID["z2"].Name != "Base Only" {
		t.Errorf("z2.Name = %q, want %q (base-only entry preserved)", byID["z2"].Name, "Base Only")
	}
	if byID["z3"].Name != "Overlay Only" {
		t.Errorf("z3.Name = %q, want %q (overlay-only entry preserved)", byID["z3"].Name, "Overlay Only")
	}
}

func TestMergeWatchlistEntriesOverlayWinsOnConflictingID(t *testing.T) {
	base := []flightmodel.WatchlistEntry{{ID: "w1", Label: "Base Label"}}
	overlay := []flightmodel.WatchlistEntry{{ID: "w1", Label: "Overlay Label"}, {ID: "w2", Label: "Overlay Only"}}

	merged := mergeWatchlistEntries(base, overlay)

	byID := make(map[string]flightmodel.WatchlistEntry, len(merged))
	for _, e := range merged {
		byID[e.ID] = e
	}
	if len(byID) != 2 {
		t.Fatalf("merged %d entries, want 2: %+v", len(byID), merged)
	}
	if byID["w1"].Label != "Overlay Label" {
		t.Errorf("w1.Label = %q, want overlay to win: %q", byID["w1"].Label, "Overlay Label")
	}
}

func TestLogFlightUpdateDoesNotPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	callsign := "UAL123"
	logFlightUpdate(logger, flightmodel.FlightState{ICAO24: "a1b2c3", Callsign: &callsign})
}

func TestLogFlightUpdateHandlesNilCallsign(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logFlightUpdate(logger, flightmodel.FlightState{ICAO24: "a1b2c3"})

	if got := buf.String(); strings.Contains(got, "0x") {
		t.Errorf("log output contains a pointer address, want dereferenced callsign: %s", got)
	}
	if got := buf.String(); !strings.Contains(got, `callsign=""`) {
		t.Errorf("log output = %s, want empty callsign field", got)
	}
}

// TestSubscriberReceivesIndependentlyOfOtherConsumers proves the eventengine
// consumer name tracks its own delivery position on flights.updates: a
// second, differently-named consumer ("other-consumer", standing in for
// e.g. the durable-store writer) also receives the same published message,
// without either one blocking or stealing the other's delivery (the
// acceptance criterion "consumers can subscribe independently").
func TestSubscriberReceivesIndependentlyOfOtherConsumers(t *testing.T) {
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

	callsign := "UAL123"
	want := flightmodel.FlightState{ICAO24: "a1b2c3", Callsign: &callsign}
	if err := natsutil.NewFlightStatePublisher(js).PublishFlightState(ctx, want); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}

	for _, name := range []string{consumerName, "other-consumer"} {
		t.Run(name, func(t *testing.T) {
			sub, err := natsutil.NewFlightStateSubscriber(ctx, js, name)
			if err != nil {
				t.Fatalf("NewFlightStateSubscriber(%s): %v", name, err)
			}

			var mu sync.Mutex
			var got *flightmodel.FlightState
			done := make(chan struct{})

			runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			go func() {
				_ = sub.Run(runCtx, nil, func(state flightmodel.FlightState, _ time.Time) {
					mu.Lock()
					if got == nil {
						got = &state
						close(done)
					}
					mu.Unlock()
				})
			}()

			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatalf("subscriber %s: timed out waiting for message", name)
			}

			mu.Lock()
			defer mu.Unlock()
			if got.ICAO24 != want.ICAO24 {
				t.Errorf("ICAO24 = %q, want %q", got.ICAO24, want.ICAO24)
			}
		})
	}
}
