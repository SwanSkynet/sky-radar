package natsutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/nats-io/nats.go/jetstream"
)

func sampleState(icao24 string) flightmodel.FlightState {
	return flightmodel.FlightState{
		ICAO24:      icao24,
		Lat:         37.0,
		Lon:         -122.0,
		LastSeenUTC: time.Now().UTC(),
	}
}

func TestFlightsUpdatesHeadSequenceEmptyStream(t *testing.T) {
	url := startTestServer(t)
	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	ctx := context.Background()
	if _, err := EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}

	seq, err := FlightsUpdatesHeadSequence(ctx, js)
	if err != nil {
		t.Fatalf("FlightsUpdatesHeadSequence: %v", err)
	}
	if seq != 0 {
		t.Errorf("seq = %d, want 0 for an empty stream", seq)
	}
}

func TestFlightsUpdatesHeadSequenceAfterPublish(t *testing.T) {
	url := startTestServer(t)
	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	ctx := context.Background()
	if _, err := EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}
	pub := NewFlightStatePublisher(js)

	for i := 0; i < 3; i++ {
		if err := pub.PublishFlightState(ctx, sampleState("aaaaaa")); err != nil {
			t.Fatalf("PublishFlightState: %v", err)
		}
	}

	seq, err := FlightsUpdatesHeadSequence(ctx, js)
	if err != nil {
		t.Fatalf("FlightsUpdatesHeadSequence: %v", err)
	}
	if seq != 3 {
		t.Errorf("seq = %d, want 3", seq)
	}
}

func TestLiveTailReaderOnlyDeliversMessagesPublishedAfterCreation(t *testing.T) {
	url := startTestServer(t)
	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	ctx := context.Background()
	if _, err := EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}
	pub := NewFlightStatePublisher(js)

	// Published before the tail reader exists: must not be delivered.
	if err := pub.PublishFlightState(ctx, sampleState("before")); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}

	reader, err := NewFlightStateLiveTailReader(ctx, js)
	if err != nil {
		t.Fatalf("NewFlightStateLiveTailReader: %v", err)
	}

	received := make(chan FlightStateMessage, 4)
	runCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		_ = reader.Run(runCtx, nil, func(msg FlightStateMessage) {
			received <- msg
		})
	}()

	// Give the consumer a moment to be fully established before publishing
	// the message it's expected to catch, so this isn't racing consumer
	// setup.
	time.Sleep(100 * time.Millisecond)
	if err := pub.PublishFlightState(ctx, sampleState("after")); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}

	select {
	case msg := <-received:
		if msg.State.ICAO24 != "after" {
			t.Fatalf("ICAO24 = %q, want %q", msg.State.ICAO24, "after")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live-tail message")
	}

	select {
	case msg := <-received:
		t.Fatalf("unexpected extra message delivered: %+v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestResumeReaderReplaysFromRequestedSequence(t *testing.T) {
	url := startTestServer(t)
	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	ctx := context.Background()
	if _, err := EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}
	pub := NewFlightStatePublisher(js)

	icao24s := []string{"aaaaaa", "bbbbbb", "cccccc", "dddddd"}
	for _, icao24 := range icao24s {
		if err := pub.PublishFlightState(ctx, sampleState(icao24)); err != nil {
			t.Fatalf("PublishFlightState: %v", err)
		}
	}

	// Client already has through sequence 2 (bbbbbb); resuming from there
	// should replay only what it missed: cccccc, dddddd. A real caller
	// sizes the fetch to the exact known gap (head - fromSeq) so it
	// doesn't block waiting for messages that don't exist.
	head, err := FlightsUpdatesHeadSequence(ctx, js)
	if err != nil {
		t.Fatalf("FlightsUpdatesHeadSequence: %v", err)
	}
	reader, err := NewFlightStateResumeReader(ctx, js, 2)
	if err != nil {
		t.Fatalf("NewFlightStateResumeReader: %v", err)
	}

	backlog, err := reader.FetchBacklog(int(head-2), 2*time.Second, nil)
	if err != nil {
		t.Fatalf("FetchBacklog: %v", err)
	}
	if len(backlog) != 2 {
		t.Fatalf("len(backlog) = %d, want 2: %+v", len(backlog), backlog)
	}
	wantOrder := []string{"cccccc", "dddddd"}
	for i, msg := range backlog {
		if msg.State.ICAO24 != wantOrder[i] {
			t.Errorf("backlog[%d].ICAO24 = %q, want %q", i, msg.State.ICAO24, wantOrder[i])
		}
		if msg.Sequence != uint64(i+3) {
			t.Errorf("backlog[%d].Sequence = %d, want %d", i, msg.Sequence, i+3)
		}
	}
}

func TestResumeReaderRejectsSequenceOlderThanRetention(t *testing.T) {
	url := startTestServer(t)
	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	ctx := context.Background()
	stream, err := EnsureFlightsUpdatesStream(ctx, js)
	if err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}
	pub := NewFlightStatePublisher(js)

	for _, icao24 := range []string{"aaaaaa", "bbbbbb", "cccccc"} {
		if err := pub.PublishFlightState(ctx, sampleState(icao24)); err != nil {
			t.Fatalf("PublishFlightState: %v", err)
		}
	}

	// Simulate retention having moved past sequence 1 by purging it off the
	// stream.
	if err := stream.Purge(ctx, jetstream.WithPurgeKeep(1)); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if _, err := NewFlightStateResumeReader(ctx, js, 1); !errors.Is(err, ErrSequenceNotRetained) {
		t.Fatalf("NewFlightStateResumeReader: err = %v, want ErrSequenceNotRetained", err)
	}
}

func TestResumeReaderRejectsSequenceOlderThanRetentionWhenStreamFullyDrained(t *testing.T) {
	url := startTestServer(t)
	nc, err := Connect(url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := JetStream(nc)
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	ctx := context.Background()
	stream, err := EnsureFlightsUpdatesStream(ctx, js)
	if err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}
	pub := NewFlightStatePublisher(js)

	for _, icao24 := range []string{"aaaaaa", "bbbbbb", "cccccc"} {
		if err := pub.PublishFlightState(ctx, sampleState(icao24)); err != nil {
			t.Fatalf("PublishFlightState: %v", err)
		}
	}

	// Purge every retained message (no WithPurgeKeep), simulating retention
	// having aged out the entire stream rather than just its oldest entries.
	if err := stream.Purge(ctx); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	if _, err := NewFlightStateResumeReader(ctx, js, 1); !errors.Is(err, ErrSequenceNotRetained) {
		t.Fatalf("NewFlightStateResumeReader: err = %v, want ErrSequenceNotRetained", err)
	}
}
