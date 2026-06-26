package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func TestLogFlightUpdateDoesNotPanic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	callsign := "UAL123"
	logFlightUpdate(logger, flightmodel.FlightState{ICAO24: "a1b2c3", Callsign: &callsign})
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
				_ = sub.Run(runCtx, nil, func(state flightmodel.FlightState) {
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
