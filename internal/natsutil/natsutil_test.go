package natsutil

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/nats-io/nats.go"
)

func testJetStream(t *testing.T) (context.Context, *nats.Conn) {
	t.Helper()

	url := startTestServer(t)
	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(nc.Close)

	return context.Background(), nc
}

func TestEnsureFlightsUpdatesStreamIsIdempotent(t *testing.T) {
	ctx, nc := testJetStream(t)

	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}

	if _, err := EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream (1st call): %v", err)
	}
	if _, err := EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream (2nd call): %v", err)
	}
}

func TestPublishFlightStateDeliversToIndependentSubscribers(t *testing.T) {
	ctx, nc := testJetStream(t)

	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	if _, err := EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}

	callsign := "UAL123"
	want := flightmodel.FlightState{
		ICAO24:      "a1b2c3",
		Callsign:    &callsign,
		Lat:         37.0,
		Lon:         -122.0,
		LastSeenUTC: time.Now().UTC(),
	}

	publisher := NewFlightStatePublisher(js)
	if err := publisher.PublishFlightState(ctx, want); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}

	// Two differently-named subscribers ("eventengine" and "pgstorewriter")
	// each get their own delivery position on the stream, demonstrating that
	// downstream consumers can subscribe independently per the Phase 2
	// acceptance criteria, rather than competing for one shared cursor.
	for _, name := range []string{"eventengine", "pgstorewriter"} {
		t.Run(name, func(t *testing.T) {
			sub, err := NewFlightStateSubscriber(ctx, js, name)
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
			if got.Callsign == nil || *got.Callsign != callsign {
				t.Errorf("Callsign = %v, want %q", got.Callsign, callsign)
			}
		})
	}
}

func TestFlightStateSubscriberSkipsMalformedMessages(t *testing.T) {
	ctx, nc := testJetStream(t)

	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	if _, err := EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}

	if _, err := js.Publish(ctx, SubjectFlightsUpdates, []byte("not json")); err != nil {
		t.Fatalf("publish malformed message: %v", err)
	}

	want := flightmodel.FlightState{ICAO24: "d4e5f6", LastSeenUTC: time.Now().UTC()}
	publisher := NewFlightStatePublisher(js)
	if err := publisher.PublishFlightState(ctx, want); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}

	sub, err := NewFlightStateSubscriber(ctx, js, "skip-malformed")
	if err != nil {
		t.Fatalf("NewFlightStateSubscriber: %v", err)
	}

	var mu sync.Mutex
	var decodeErrs int
	var got *flightmodel.FlightState
	done := make(chan struct{})

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	go func() {
		_ = sub.Run(runCtx, func(error) {
			mu.Lock()
			decodeErrs++
			mu.Unlock()
		}, func(state flightmodel.FlightState) {
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
		t.Fatal("timed out waiting for valid message after malformed one")
	}

	mu.Lock()
	defer mu.Unlock()
	if decodeErrs != 1 {
		t.Errorf("decodeErrs = %d, want 1", decodeErrs)
	}
	if got.ICAO24 != want.ICAO24 {
		t.Errorf("ICAO24 = %q, want %q", got.ICAO24, want.ICAO24)
	}
}
