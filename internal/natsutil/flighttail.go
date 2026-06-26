package natsutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/nats-io/nats.go/jetstream"
)

// ErrSequenceNotRetained is returned by NewFlightStateResumeReader when the
// requested resume point has fallen out of FLIGHTS_UPDATES' retention
// window. Callers (the API gateway) should treat this as "resume isn't
// possible" and fall back to a full state reload rather than treating it
// as a transient error.
var ErrSequenceNotRetained = errors.New("natsutil: requested sequence is no longer retained")

// FlightStateMessage pairs a decoded FlightState with the JetStream stream
// sequence number it was delivered at. The API gateway hands this sequence
// back to clients so a reconnecting client can ask to resume from exactly
// where it left off (see docs/prd/phase-2-realtime-systems.md P2-FR4).
type FlightStateMessage struct {
	Sequence uint64
	State    flightmodel.FlightState
}

// FlightStateTailHandler is invoked once per decoded FlightStateMessage
// delivered to a FlightStateTailReader's Run loop.
type FlightStateTailHandler func(FlightStateMessage)

// FlightStateTailReader is an ephemeral, ordered JetStream consumer of
// SubjectFlightsUpdates. It is distinct from FlightStateSubscriber's
// durable named consumers: a tail reader is created fresh per use (a
// gateway instance's own live broadcast feed, or a single reconnecting
// client's gap replay) and carries no durable delivery-position state of
// its own — the caller (the gateway, on behalf of its connected clients)
// is responsible for remembering a resume position, not JetStream.
type FlightStateTailReader struct {
	consumer jetstream.Consumer
}

// NewFlightStateLiveTailReader creates an ephemeral ordered consumer that
// delivers only messages published from this point forward. Each gateway
// instance creates its own so instances don't compete for the same
// messages, matching "subscribes to flights.updates once per gateway
// instance and fans out" in docs/architecture/system-architecture.md.
func NewFlightStateLiveTailReader(ctx context.Context, js jetstream.JetStream) (*FlightStateTailReader, error) {
	consumer, err := js.OrderedConsumer(ctx, StreamFlightsUpdates, jetstream.OrderedConsumerConfig{
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("natsutil: live tail reader: %w", err)
	}
	return &FlightStateTailReader{consumer: consumer}, nil
}

// NewFlightStateResumeReader creates an ephemeral ordered consumer that
// replays messages starting immediately after fromSeq, for filling the gap
// a reconnecting WebSocket client missed while disconnected. It returns
// ErrSequenceNotRetained if fromSeq has already fallen out of the stream's
// retained window, so the caller can fall back to a full state reload
// instead of silently skipping missed updates.
func NewFlightStateResumeReader(ctx context.Context, js jetstream.JetStream, fromSeq uint64) (*FlightStateTailReader, error) {
	stream, err := js.Stream(ctx, StreamFlightsUpdates)
	if err != nil {
		return nil, fmt.Errorf("natsutil: resume reader: lookup stream: %w", err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("natsutil: resume reader: stream info: %w", err)
	}
	// Gate on FirstSeq > 0 rather than Msgs > 0: a stream that has been
	// fully purged has Msgs == 0 but FirstSeq == LastSeq+1, and an old
	// fromSeq must still be rejected in that case. FirstSeq == 0 only for a
	// stream that has never had a message published, where there's no
	// retention boundary to violate.
	if info.State.FirstSeq > 0 && fromSeq < info.State.FirstSeq-1 {
		return nil, ErrSequenceNotRetained
	}

	consumer, err := js.OrderedConsumer(ctx, StreamFlightsUpdates, jetstream.OrderedConsumerConfig{
		DeliverPolicy: jetstream.DeliverByStartSequencePolicy,
		OptStartSeq:   fromSeq + 1,
	})
	if err != nil {
		return nil, fmt.Errorf("natsutil: resume reader: %w", err)
	}
	return &FlightStateTailReader{consumer: consumer}, nil
}

// FlightsUpdatesHeadSequence returns the sequence number of the most
// recently published message on FLIGHTS_UPDATES (0 if the stream is
// empty), so a newly subscribed client can record it as the baseline to
// resume from on a future reconnect.
func FlightsUpdatesHeadSequence(ctx context.Context, js jetstream.JetStream) (uint64, error) {
	stream, err := js.Stream(ctx, StreamFlightsUpdates)
	if err != nil {
		return 0, fmt.Errorf("natsutil: head sequence: lookup stream: %w", err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return 0, fmt.Errorf("natsutil: head sequence: stream info: %w", err)
	}
	return info.State.LastSeq, nil
}

// Run decodes each message delivered to this reader and passes it to
// handler. A message that fails to decode, or whose metadata can't be
// read, is reported to onErr (if non-nil) and skipped rather than stopping
// the reader. Run blocks until ctx is done, so it's meant for a long-lived
// live tail; resume replay should use FetchBacklog instead, which returns
// once the requested batch is drained.
func (r *FlightStateTailReader) Run(ctx context.Context, onErr func(error), handler FlightStateTailHandler) error {
	consumeCtx, err := r.consumer.Consume(func(msg jetstream.Msg) {
		fsMsg, ok := decodeFlightStateMessage(msg, onErr)
		if !ok {
			return
		}
		handler(fsMsg)
	})
	if err != nil {
		return fmt.Errorf("natsutil: consume: %w", err)
	}
	defer consumeCtx.Stop()

	<-ctx.Done()
	return nil
}

// FetchBacklog pulls up to maxMsgs already-retained messages from this
// reader, waiting at most maxWait for the batch to fill, and returns them
// decoded and in stream order. It's meant for a bounded resume replay (the
// caller knows the exact gap size from the stream's head sequence), unlike
// Run's indefinite live tail. A message that fails to decode is reported
// to onErr (if non-nil) and omitted from the result rather than failing
// the whole fetch.
func (r *FlightStateTailReader) FetchBacklog(maxMsgs int, maxWait time.Duration, onErr func(error)) ([]FlightStateMessage, error) {
	if maxMsgs <= 0 {
		return nil, nil
	}

	batch, err := r.consumer.Fetch(maxMsgs, jetstream.FetchMaxWait(maxWait))
	if err != nil {
		return nil, fmt.Errorf("natsutil: fetch backlog: %w", err)
	}

	out := make([]FlightStateMessage, 0, maxMsgs)
	for msg := range batch.Messages() {
		if fsMsg, ok := decodeFlightStateMessage(msg, onErr); ok {
			out = append(out, fsMsg)
		}
	}
	if err := batch.Error(); err != nil {
		return out, fmt.Errorf("natsutil: fetch backlog: %w", err)
	}
	return out, nil
}

// decodeFlightStateMessage decodes msg's payload and stream sequence into
// a FlightStateMessage, reporting any failure to onErr and returning
// ok=false so the caller can skip the message rather than abort.
func decodeFlightStateMessage(msg jetstream.Msg, onErr func(error)) (FlightStateMessage, bool) {
	meta, err := msg.Metadata()
	if err != nil {
		if onErr != nil {
			onErr(fmt.Errorf("natsutil: message metadata: %w", err))
		}
		return FlightStateMessage{}, false
	}
	var state flightmodel.FlightState
	if err := json.Unmarshal(msg.Data(), &state); err != nil {
		if onErr != nil {
			onErr(fmt.Errorf("natsutil: decode flight state: %w", err))
		}
		return FlightStateMessage{}, false
	}
	return FlightStateMessage{Sequence: meta.Sequence.Stream, State: state}, true
}
