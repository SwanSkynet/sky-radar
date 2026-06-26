package natsutil

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
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

func TestEnsureEventsDetectedStreamIsIdempotent(t *testing.T) {
	ctx, nc := testJetStream(t)

	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}

	if _, err := EnsureEventsDetectedStream(ctx, js); err != nil {
		t.Fatalf("EnsureEventsDetectedStream (1st call): %v", err)
	}
	if _, err := EnsureEventsDetectedStream(ctx, js); err != nil {
		t.Fatalf("EnsureEventsDetectedStream (2nd call): %v", err)
	}
}

func TestPublishEventDeliversToSubscriber(t *testing.T) {
	ctx, nc := testJetStream(t)

	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	if _, err := EnsureEventsDetectedStream(ctx, js); err != nil {
		t.Fatalf("EnsureEventsDetectedStream: %v", err)
	}

	want := flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeStaleSignal,
		ICAO24:        "a1b2c3",
		Severity:      flightmodel.EventSeverityWarning,
		OccurredAtUTC: time.Now().UTC(),
		Detail:        json.RawMessage(`{"stale_for_seconds":90}`),
	}

	publisher := NewEventPublisher(js)
	if err := publisher.PublishEvent(ctx, want); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	stream, err := js.Stream(ctx, StreamEventsDetected)
	if err != nil {
		t.Fatalf("js.Stream: %v", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "test-consumer",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateConsumer: %v", err)
	}

	msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(4*time.Second))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	var got *flightmodel.Event
	for msg := range msgs.Messages() {
		var event flightmodel.Event
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		got = &event
		if err := msg.Ack(); err != nil {
			t.Fatalf("Ack: %v", err)
		}
	}
	if msgs.Error() != nil {
		t.Fatalf("Fetch: %v", msgs.Error())
	}
	if got == nil {
		t.Fatal("no event received")
	}
	if got.ID != want.ID || got.Type != want.Type || got.ICAO24 != want.ICAO24 {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestEventSubscriberDeliversEvent(t *testing.T) {
	ctx, nc := testJetStream(t)

	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	if _, err := EnsureEventsDetectedStream(ctx, js); err != nil {
		t.Fatalf("EnsureEventsDetectedStream: %v", err)
	}

	want := flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeGeofenceEnter,
		ICAO24:        "a1b2c3",
		Severity:      flightmodel.EventSeverityInfo,
		OccurredAtUTC: time.Now().UTC(),
		Detail:        json.RawMessage(`{"zone_id":"zone-1"}`),
	}

	publisher := NewEventPublisher(js)
	if err := publisher.PublishEvent(ctx, want); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	sub, err := NewEventSubscriber(ctx, js, "pgstorewriter-events")
	if err != nil {
		t.Fatalf("NewEventSubscriber: %v", err)
	}

	var mu sync.Mutex
	var got *flightmodel.Event
	done := make(chan struct{})

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	go func() {
		_ = sub.Run(runCtx, nil, func(event flightmodel.Event) {
			mu.Lock()
			if got == nil {
				got = &event
				close(done)
			}
			mu.Unlock()
		})
	}()

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	mu.Lock()
	defer mu.Unlock()
	if got.ID != want.ID || got.Type != want.Type || got.ICAO24 != want.ICAO24 {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestEventSubscriberSkipsMalformedMessages(t *testing.T) {
	ctx, nc := testJetStream(t)

	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	if _, err := EnsureEventsDetectedStream(ctx, js); err != nil {
		t.Fatalf("EnsureEventsDetectedStream: %v", err)
	}

	if _, err := js.Publish(ctx, SubjectEventsDetected, []byte("not json")); err != nil {
		t.Fatalf("publish malformed message: %v", err)
	}

	want := flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeWatchlistMatch,
		ICAO24:        "d4e5f6",
		Severity:      flightmodel.EventSeverityInfo,
		OccurredAtUTC: time.Now().UTC(),
	}
	publisher := NewEventPublisher(js)
	if err := publisher.PublishEvent(ctx, want); err != nil {
		t.Fatalf("PublishEvent: %v", err)
	}

	sub, err := NewEventSubscriber(ctx, js, "skip-malformed-events")
	if err != nil {
		t.Fatalf("NewEventSubscriber: %v", err)
	}

	var mu sync.Mutex
	var decodeErrs int
	var got *flightmodel.Event
	done := make(chan struct{})

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	go func() {
		_ = sub.Run(runCtx, func(error) {
			mu.Lock()
			decodeErrs++
			mu.Unlock()
		}, func(event flightmodel.Event) {
			mu.Lock()
			if got == nil {
				got = &event
				close(done)
			}
			mu.Unlock()
		})
	}()

	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for valid event after malformed one")
	}

	mu.Lock()
	defer mu.Unlock()
	if decodeErrs != 1 {
		t.Errorf("decodeErrs = %d, want 1", decodeErrs)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
}
