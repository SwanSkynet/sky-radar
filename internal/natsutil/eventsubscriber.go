package natsutil

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/nats-io/nats.go/jetstream"
)

// EventHandler is invoked once per decoded Event delivered to an
// EventSubscriber. A non-nil return causes the message to be redelivered
// rather than acknowledged.
type EventHandler func(flightmodel.Event) error

// EventSubscriber is a durable JetStream consumer of SubjectEventsDetected.
// Each consumer name tracks its own delivery position on the stream,
// mirroring FlightStateSubscriber, so any number of subscribers (the
// durable-store writer, API gateway notifications) can read
// events.detected independently.
type EventSubscriber struct {
	consumer jetstream.Consumer
}

// NewEventSubscriber creates (or reuses, if one with this name already
// exists) a durable pull consumer named consumerName on the
// EVENTS_DETECTED stream. EnsureEventsDetectedStream must have been called
// first so the stream exists.
func NewEventSubscriber(ctx context.Context, js jetstream.JetStream, consumerName string) (*EventSubscriber, error) {
	stream, err := js.Stream(ctx, StreamEventsDetected)
	if err != nil {
		return nil, fmt.Errorf("natsutil: subscriber %s: lookup stream %s: %w", consumerName, StreamEventsDetected, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       consumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("natsutil: subscriber %s: create consumer: %w", consumerName, err)
	}

	return &EventSubscriber{consumer: consumer}, nil
}

// Run decodes each message delivered to this subscriber into an Event and
// passes it to handler. A message that fails to decode is reported to
// onErr (if non-nil) and acked rather than redelivered forever, mirroring
// FlightStateSubscriber.Run. A message whose handler returns an error is
// reported to onErr and Nak'd so JetStream redelivers it instead of losing
// it; only a successful handler call acknowledges the message. Run blocks
// until ctx is done.
func (s *EventSubscriber) Run(ctx context.Context, onErr func(error), handler EventHandler) error {
	consumeCtx, err := s.consumer.Consume(func(msg jetstream.Msg) {
		var event flightmodel.Event
		if err := json.Unmarshal(msg.Data(), &event); err != nil {
			if onErr != nil {
				onErr(fmt.Errorf("natsutil: decode event: %w", err))
			}
			if ackErr := msg.Ack(); ackErr != nil && onErr != nil {
				onErr(fmt.Errorf("natsutil: ack message: %w", ackErr))
			}
			return
		}
		if err := handler(event); err != nil {
			if onErr != nil {
				onErr(fmt.Errorf("natsutil: handle event: %w", err))
			}
			if nakErr := msg.Nak(); nakErr != nil && onErr != nil {
				onErr(fmt.Errorf("natsutil: nak message: %w", nakErr))
			}
			return
		}
		if ackErr := msg.Ack(); ackErr != nil && onErr != nil {
			onErr(fmt.Errorf("natsutil: ack message: %w", ackErr))
		}
	})
	if err != nil {
		return fmt.Errorf("natsutil: consume: %w", err)
	}
	defer consumeCtx.Stop()

	<-ctx.Done()
	return nil
}
