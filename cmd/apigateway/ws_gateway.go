package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// wsSubscribeTimeout bounds how long the gateway waits for a freshly
	// accepted connection to send its first subscribe message before
	// giving up on it.
	wsSubscribeTimeout = 10 * time.Second

	// wsBacklogFetchTimeout bounds how long a resume replay waits for
	// JetStream to deliver the requested backlog before giving up.
	wsBacklogFetchTimeout = 5 * time.Second
)

// wsMaxResumeBacklog caps how many messages a single resume replay will
// fetch. Retention is time-based (see flightsUpdatesMaxAge), so a stale
// fromSeq can still be "retained" but imply a gap of tens of thousands of
// messages; without this cap that gap size drives an unbounded allocation
// and replay burst. A resume whose gap exceeds it is treated the same as
// one that's fallen out of retention: resume_failed, fall back to a full
// reload. It's a var rather than a const so tests can shrink it instead of
// publishing tens of thousands of messages to exercise the cap.
var wsMaxResumeBacklog uint64 = 20000

// wsGateway serves GET /ws: it accepts a WebSocket connection, registers
// it with hub by viewport, optionally replays a resume gap from
// JetStream, and then streams hub-filtered flights.updates to the client
// until it disconnects. It owns transport and filtering only — no
// business logic — per the API gateway's responsibility boundary in
// docs/architecture/system-architecture.md.
type wsGateway struct {
	hub    *wsHub
	js     jetstream.JetStream
	logger *slog.Logger
}

func newWSGateway(hub *wsHub, js jetstream.JetStream, logger *slog.Logger) *wsGateway {
	return &wsGateway{hub: hub, js: js, logger: logger}
}

func (g *wsGateway) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		g.logger.Error("websocket accept failed", "err", err)
		return
	}
	defer func() { _ = conn.CloseNow() }()

	ctx := r.Context()
	client, lastDeliveredSeq, err := g.handshake(ctx, conn)
	if err != nil {
		g.logger.Warn("websocket handshake failed", "err", err)
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	defer g.hub.unregister(client)

	g.serve(ctx, conn, client, lastDeliveredSeq)
}

// handshake reads the client's first message, validates it as a
// subscribe, registers the client's viewport with the hub, and (if
// requested) replays a resume gap. It returns the sequence number up to
// which the client has already received updates, so serve's live-tail
// drain loop can discard anything at or below that as already delivered.
func (g *wsGateway) handshake(ctx context.Context, conn *websocket.Conn) (*wsClient, uint64, error) {
	readCtx, cancel := context.WithTimeout(ctx, wsSubscribeTimeout)
	defer cancel()

	var msg wsClientMessage
	if err := wsjson.Read(readCtx, conn, &msg); err != nil {
		return nil, 0, fmt.Errorf("read subscribe message: %w", err)
	}
	if msg.Type != wsMsgTypeSubscribe {
		return nil, 0, fmt.Errorf("first message must be %q, got %q", wsMsgTypeSubscribe, msg.Type)
	}
	bbox, err := bboxFromSlice(msg.BBox)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid bbox: %w", err)
	}

	client := newWSClient()
	client.SetBBox(bbox)

	// Register before computing the head sequence below: any message
	// published concurrently with that snapshot is then queued for live
	// delivery rather than missed entirely. serve's high-water-mark check
	// against lastDeliveredSeq discards the resulting at-most-once
	// duplicate instead of delivering it twice.
	g.hub.register(client)

	headSeq, err := natsutil.FlightsUpdatesHeadSequence(ctx, g.js)
	if err != nil {
		g.hub.unregister(client)
		return nil, 0, fmt.Errorf("head sequence: %w", err)
	}

	lastDeliveredSeq := headSeq
	if msg.ResumeFromSeq != nil {
		lastDeliveredSeq, err = g.replayResume(ctx, conn, client, *msg.ResumeFromSeq, headSeq)
		if err != nil {
			g.hub.unregister(client)
			return nil, 0, err
		}
	}

	if err := wsjson.Write(ctx, conn, wsServerMessage{Type: wsMsgTypeSubscribed, Seq: lastDeliveredSeq}); err != nil {
		g.hub.unregister(client)
		return nil, 0, fmt.Errorf("write subscribed ack: %w", err)
	}

	return client, lastDeliveredSeq, nil
}

// replayResume replays the gap between fromSeq (the sequence the client
// already has) and headSeq (the stream's head at registration time),
// writing only the entries that fall inside client's currently registered
// viewport — the same filter wsHub.broadcast applies to live updates, so a
// resume can't leak out-of-viewport state the live path would have
// dropped. If fromSeq is no longer retained, or the gap exceeds
// wsMaxResumeBacklog, it writes a resume_failed message and returns
// headSeq unchanged, telling the caller "nothing was replayed, the client
// must fall back to a full reload" rather than treating that as a
// connection-ending error — only a transport (write) failure is returned
// as an error here.
func (g *wsGateway) replayResume(ctx context.Context, conn *websocket.Conn, client *wsClient, fromSeq, headSeq uint64) (uint64, error) {
	if fromSeq >= headSeq {
		return fromSeq, nil
	}

	if headSeq-fromSeq > wsMaxResumeBacklog {
		if writeErr := wsjson.Write(ctx, conn, wsServerMessage{Type: wsMsgTypeResumeFailed, Reason: "resume gap exceeds maximum replay window"}); writeErr != nil {
			return 0, fmt.Errorf("write resume_failed: %w", writeErr)
		}
		return headSeq, nil
	}

	reader, err := natsutil.NewFlightStateResumeReader(ctx, g.js, fromSeq)
	if err != nil {
		reason := "resume temporarily unavailable"
		if errors.Is(err, natsutil.ErrSequenceNotRetained) {
			reason = err.Error()
		} else {
			g.logger.Error("resume reader setup failed", "err", err)
		}
		if writeErr := wsjson.Write(ctx, conn, wsServerMessage{Type: wsMsgTypeResumeFailed, Reason: reason}); writeErr != nil {
			return 0, fmt.Errorf("write resume_failed: %w", writeErr)
		}
		return headSeq, nil
	}

	backlog, err := reader.FetchBacklog(int(headSeq-fromSeq), wsBacklogFetchTimeout, func(decodeErr error) {
		g.logger.Error("resume backlog decode error", "err", decodeErr)
	})
	if err != nil {
		g.logger.Error("resume backlog fetch failed", "err", err)
		if writeErr := wsjson.Write(ctx, conn, wsServerMessage{Type: wsMsgTypeResumeFailed, Reason: "resume temporarily unavailable"}); writeErr != nil {
			return 0, fmt.Errorf("write resume_failed: %w", writeErr)
		}
		return headSeq, nil
	}

	for _, fsMsg := range backlog {
		if !client.BBox().Contains(fsMsg.State.Lat, fsMsg.State.Lon) {
			continue
		}
		if err := wsjson.Write(ctx, conn, wsServerMessage{Type: wsMsgTypeFlightUpdate, Seq: fsMsg.Sequence, State: &fsMsg.State}); err != nil {
			return 0, fmt.Errorf("write resume backlog message: %w", err)
		}
		wsMessagesSent.Add(ctx, 1)
	}

	if len(backlog) == 0 {
		return fromSeq, nil
	}
	return backlog[len(backlog)-1].Sequence, nil
}

// serve streams hub-filtered flights.updates to conn until the client
// disconnects or the request context ends. lastDeliveredSeq is the
// high-water mark of what's already reached the client (from the
// subscribe ack / resume replay); any hub message at or below it is a
// duplicate (see the registration-ordering note in handshake) and is
// dropped rather than re-sent.
func (g *wsGateway) serve(ctx context.Context, conn *websocket.Conn, client *wsClient, lastDeliveredSeq uint64) {
	readErrCh := make(chan error, 1)
	go g.readViewportUpdates(ctx, conn, client, readErrCh)

	for {
		select {
		case <-ctx.Done():
			return
		case err := <-readErrCh:
			if err != nil && websocket.CloseStatus(err) == -1 {
				g.logger.Warn("websocket read failed", "err", err)
			}
			return
		case fsMsg := <-client.send:
			if fsMsg.Sequence <= lastDeliveredSeq {
				continue
			}
			if err := wsjson.Write(ctx, conn, wsServerMessage{Type: wsMsgTypeFlightUpdate, Seq: fsMsg.Sequence, State: &fsMsg.State}); err != nil {
				g.logger.Warn("websocket write failed", "err", err)
				return
			}
			wsMessagesSent.Add(ctx, 1)
			lastDeliveredSeq = fsMsg.Sequence
		}
	}
}

// readViewportUpdates reads subsequent client messages for the life of
// the connection. The only message clients send after the initial
// handshake is another "subscribe", reused to mean "update my registered
// viewport" (e.g. the user panned/zoomed the map) rather than introducing
// a second message type. A malformed update is ignored rather than
// closing the connection; any read error (including a normal client-
// initiated close) is sent to errCh and ends the loop.
func (g *wsGateway) readViewportUpdates(ctx context.Context, conn *websocket.Conn, client *wsClient, errCh chan<- error) {
	for {
		var msg wsClientMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			errCh <- err
			return
		}
		if msg.Type != wsMsgTypeSubscribe {
			continue
		}
		if bbox, err := bboxFromSlice(msg.BBox); err == nil {
			client.SetBBox(bbox)
		}
	}
}
