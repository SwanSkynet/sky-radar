package natsutil

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// SubjectFlightsUpdates is the subject the normalizer publishes
	// canonical FlightState updates to; see the subject table in
	// docs/tech-stack/data-and-messaging.md.
	SubjectFlightsUpdates = "flights.updates"

	// StreamFlightsUpdates is the JetStream stream backing
	// SubjectFlightsUpdates.
	StreamFlightsUpdates = "FLIGHTS_UPDATES"

	// flightsUpdatesMaxAge matches the "full replay window (default 30
	// minutes full-resolution)" retention documented for flights.updates in
	// docs/tech-stack/data-and-messaging.md.
	flightsUpdatesMaxAge = 30 * time.Minute
)

// Connect dials the NATS server at url. Callers should treat a failure here
// like a Redis ping failure elsewhere in this codebase (fail fast at
// startup rather than serving traffic without a working stream bus).
func Connect(url string, opts ...nats.Option) (*nats.Conn, error) {
	nc, err := nats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("natsutil: connect: %w", err)
	}
	return nc, nil
}

// JetStream wraps nc in a jetstream.JetStream context for stream/consumer management.
func JetStream(nc *nats.Conn) (jetstream.JetStream, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("natsutil: jetstream context: %w", err)
	}
	return js, nil
}

// EnsureFlightsUpdatesStream creates the FLIGHTS_UPDATES stream if it does
// not already exist, or updates it to match if it does. It is idempotent
// and safe to call from both the producer (normalizer) and any consumer at
// startup, so neither side depends on the other having started first.
func EnsureFlightsUpdatesStream(ctx context.Context, js jetstream.JetStream) (jetstream.Stream, error) {
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamFlightsUpdates,
		Subjects:  []string{SubjectFlightsUpdates},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    flightsUpdatesMaxAge,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		return nil, fmt.Errorf("natsutil: ensure stream %s: %w", StreamFlightsUpdates, err)
	}
	return stream, nil
}
