package natsutil

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/nats-io/nats.go/jetstream"
)

// eventRedeliverDelay spaces out NAK-based redeliveries for a failed
// handler call so a persistent failure (e.g. the durable store being down)
// backs off instead of hot-looping redelivery against the broker.
//
// eventMaxDeliver bounds how many times JetStream will redeliver an event
// message to a failing handler before it is given up on (Term'd) rather
// than retried forever.
//
// Both are vars rather than consts so tests can shrink them for fast,
// deterministic exercising of the give-up path.
var (
	eventRedeliverDelay        = 5 * time.Second
	eventMaxDeliver     uint64 = 10
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
		MaxDeliver:    int(eventMaxDeliver),
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
// reported to onErr and NakWithDelay'd (spaced by eventRedeliverDelay) so a
// persistent failure backs off instead of hot-looping redelivery; once the
// consumer's MaxDeliver attempts are exhausted the message is Term'd and
// reported to onErr instead of being redelivered forever. Only a successful
// handler call acknowledges the message. Run blocks until ctx is done.
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
			if meta, metaErr := msg.Metadata(); metaErr == nil && meta.NumDelivered >= eventMaxDeliver {
				if termErr := msg.Term(); termErr != nil && onErr != nil {
					onErr(fmt.Errorf("natsutil: term message after %d delivery attempts: %w", meta.NumDelivered, termErr))
				} else if onErr != nil {
					onErr(fmt.Errorf("natsutil: giving up on event after %d delivery attempts", meta.NumDelivered))
				}
				return
			}
			if nakErr := msg.NakWithDelay(eventRedeliverDelay); nakErr != nil && onErr != nil {
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
