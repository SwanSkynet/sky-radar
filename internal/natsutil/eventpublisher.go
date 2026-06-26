package natsutil

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/nats-io/nats.go/jetstream"
)

// EventPublisher publishes detected Events to SubjectEventsDetected. The
// event engine is the sole owner of an instance of this type, per the
// producer boundary in docs/architecture/system-architecture.md.
type EventPublisher struct {
	js jetstream.JetStream
}

// NewEventPublisher wraps js for publishing Events. EnsureEventsDetectedStream
// must have been called (by the caller) so the stream this publishes to
// already exists.
func NewEventPublisher(js jetstream.JetStream) *EventPublisher {
	return &EventPublisher{js: js}
}

// PublishEvent marshals event to JSON and publishes it to
// SubjectEventsDetected, blocking until JetStream acknowledges the write.
func (p *EventPublisher) PublishEvent(ctx context.Context, event flightmodel.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("natsutil: marshal event %s: %w", event.ID, err)
	}
	if _, err := p.js.Publish(ctx, SubjectEventsDetected, data); err != nil {
		return fmt.Errorf("natsutil: publish event %s: %w", event.ID, err)
	}
	return nil
}
