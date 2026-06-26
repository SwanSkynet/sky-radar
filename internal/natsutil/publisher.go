package natsutil

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/nats-io/nats.go/jetstream"
)

// FlightStatePublisher publishes canonical FlightState updates to
// SubjectFlightsUpdates. The normalizer is the sole owner of an instance of
// this type, per the producer boundary in docs/architecture/system-architecture.md.
type FlightStatePublisher struct {
	js jetstream.JetStream
}

// NewFlightStatePublisher wraps js for publishing FlightState updates.
// EnsureFlightsUpdatesStream must have been called (by the caller) so the
// stream this publishes to already exists.
func NewFlightStatePublisher(js jetstream.JetStream) *FlightStatePublisher {
	return &FlightStatePublisher{js: js}
}

// PublishFlightState marshals state to JSON and publishes it to
// SubjectFlightsUpdates, blocking until JetStream acknowledges the write.
func (p *FlightStatePublisher) PublishFlightState(ctx context.Context, state flightmodel.FlightState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("natsutil: marshal flight state %s: %w", state.ICAO24, err)
	}
	if _, err := p.js.Publish(ctx, SubjectFlightsUpdates, data); err != nil {
		return fmt.Errorf("natsutil: publish flight state %s: %w", state.ICAO24, err)
	}
	return nil
}
