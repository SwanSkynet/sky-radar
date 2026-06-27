package natsutil

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/nats-io/nats.go/jetstream"
)

// FlightStateHandler is invoked once per decoded FlightState delivered to a
// FlightStateSubscriber. ingestedAt is the JetStream message's publish
// timestamp — i.e. when the normalizer published it to flights.updates —
// distinct from the FlightState's own LastSeenUTC (the source's
// observation time); subscribers use it to measure their own
// processing/consumer lag against the normalized-ingest point, per the
// master PRD's event-detection-latency and DR-RPO SLOs.
type FlightStateHandler func(state flightmodel.FlightState, ingestedAt time.Time)

// FlightStateSubscriber is a durable JetStream consumer of
// SubjectFlightsUpdates. Each consumer name tracks its own delivery
// position on the stream, so any number of subscribers (event engine,
// durable-store writer, API gateway) can read flights.updates independently
// without competing for messages or needing to coordinate with each other
// or with the normalizer — see "Why ingestion is decoupled from serving" in
// docs/architecture/system-architecture.md.
type FlightStateSubscriber struct {
	consumer jetstream.Consumer
}

// NewFlightStateSubscriber creates (or reuses, if one with this name
// already exists) a durable pull consumer named consumerName on the
// FLIGHTS_UPDATES stream. EnsureFlightsUpdatesStream must have been called
// first so the stream exists.
func NewFlightStateSubscriber(ctx context.Context, js jetstream.JetStream, consumerName string) (*FlightStateSubscriber, error) {
	stream, err := js.Stream(ctx, StreamFlightsUpdates)
	if err != nil {
		return nil, fmt.Errorf("natsutil: subscriber %s: lookup stream %s: %w", consumerName, StreamFlightsUpdates, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       consumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("natsutil: subscriber %s: create consumer: %w", consumerName, err)
	}

	return &FlightStateSubscriber{consumer: consumer}, nil
}

// Run decodes each message delivered to this subscriber into a
// FlightState and passes it to handler, acknowledging the message
// afterward. A message that fails to decode is reported to onErr (if
// non-nil) and acked rather than redelivered forever, mirroring the
// skip-and-continue fault isolation redisutil.ScanRawStates uses for
// malformed entries. Run blocks until ctx is done.
func (s *FlightStateSubscriber) Run(ctx context.Context, onErr func(error), handler FlightStateHandler) error {
	consumeCtx, err := s.consumer.Consume(func(msg jetstream.Msg) {
		var state flightmodel.FlightState
		if err := json.Unmarshal(msg.Data(), &state); err != nil {
			if onErr != nil {
				onErr(fmt.Errorf("natsutil: decode flight state: %w", err))
			}
			if ackErr := msg.Ack(); ackErr != nil && onErr != nil {
				onErr(fmt.Errorf("natsutil: ack message: %w", ackErr))
			}
			return
		}
		ingestedAt := time.Now()
		if meta, err := msg.Metadata(); err == nil {
			if !meta.Timestamp.After(ingestedAt) {
				ingestedAt = meta.Timestamp
			}
		} else if onErr != nil {
			onErr(fmt.Errorf("natsutil: message metadata: %w", err))
		}
		handler(state, ingestedAt)
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
