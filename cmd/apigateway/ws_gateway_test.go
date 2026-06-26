package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// testWSEnv boots an in-process NATS server with JetStream, wires up a
// wsGateway against it exactly as main() would (including the live-tail
// broadcast goroutine), and serves it over an httptest server. It returns
// the ws:// URL for GET /ws, a publisher for injecting flights.updates
// messages as the normalizer would, and the raw JetStream context for
// tests that need to manipulate stream retention directly.
func testWSEnv(t *testing.T) (wsURL string, pub *natsutil.FlightStatePublisher, js jetstream.JetStream) {
	t.Helper()

	srv, err := server.NewServer(&server.Options{
		Host:      "127.0.0.1",
		Port:      -1,
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("server.NewServer: %v", err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats test server: not ready for connections")
	}

	nc, err := natsutil.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("natsutil.Connect: %v", err)
	}
	t.Cleanup(nc.Close)

	js, err = natsutil.JetStream(nc)
	if err != nil {
		t.Fatalf("natsutil.JetStream: %v", err)
	}

	ctx := context.Background()
	if _, err := natsutil.EnsureFlightsUpdatesStream(ctx, js); err != nil {
		t.Fatalf("EnsureFlightsUpdatesStream: %v", err)
	}

	hub := newWSHub()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wsGW := newWSGateway(hub, js, logger)

	liveTail, err := natsutil.NewFlightStateLiveTailReader(ctx, js)
	if err != nil {
		t.Fatalf("NewFlightStateLiveTailReader: %v", err)
	}
	tailCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = liveTail.Run(tailCtx, nil, hub.broadcast) }()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", wsGW.handleWS)
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(httpSrv.Close)

	wsURL = "ws" + strings.TrimPrefix(httpSrv.URL, "http") + "/ws"
	pub = natsutil.NewFlightStatePublisher(js)
	return wsURL, pub, js
}

func dialWS(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(websocket.StatusNormalClosure, "") })
	return conn
}

func subscribe(t *testing.T, conn *websocket.Conn, bbox []float64, resumeFromSeq *uint64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	msg := wsClientMessage{Type: wsMsgTypeSubscribe, BBox: bbox, ResumeFromSeq: resumeFromSeq}
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatalf("wsjson.Write(subscribe): %v", err)
	}
}

func readServerMessage(t *testing.T, conn *websocket.Conn, timeout time.Duration) wsServerMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var msg wsServerMessage
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("wsjson.Read: %v", err)
	}
	return msg
}

func mustReadType(t *testing.T, conn *websocket.Conn, wantType string) wsServerMessage {
	t.Helper()
	msg := readServerMessage(t, conn, 3*time.Second)
	if msg.Type != wantType {
		t.Fatalf("message type = %q, want %q (full message: %+v)", msg.Type, wantType, msg)
	}
	return msg
}

func readFlightUpdate(t *testing.T, conn *websocket.Conn, timeout time.Duration) wsServerMessage {
	t.Helper()
	msg := readServerMessage(t, conn, timeout)
	if msg.Type != wsMsgTypeFlightUpdate {
		t.Fatalf("message type = %q, want %q (full message: %+v)", msg.Type, wsMsgTypeFlightUpdate, msg)
	}
	if msg.State == nil {
		t.Fatalf("flight_update message has a nil state: %+v", msg)
	}
	return msg
}

// assertNoMoreMessages waits up to wait for another message on conn and
// fails the test if one arrives. Per nhooyr.io/websocket's documented
// behavior, a context-deadline read failure closes the connection, so
// this must be the last thing a test does with conn.
func assertNoMoreMessages(t *testing.T, conn *websocket.Conn, wait time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	var msg wsServerMessage
	if err := wsjson.Read(ctx, conn, &msg); err == nil {
		t.Fatalf("unexpected message received: %+v", msg)
	}
}

func sampleFlightAt(icao24 string, lat, lon float64) flightmodel.FlightState {
	return flightmodel.FlightState{ICAO24: icao24, Lat: lat, Lon: lon, LastSeenUTC: time.Now().UTC()}
}

func TestWSDeliversOnlyInViewportUpdates(t *testing.T) {
	wsURL, pub, _ := testWSEnv(t)
	ctx := context.Background()

	connSF := dialWS(t, wsURL)
	subscribe(t, connSF, []float64{-123, 36, -121, 38}, nil)
	mustReadType(t, connSF, wsMsgTypeSubscribed)

	connNY := dialWS(t, wsURL)
	subscribe(t, connNY, []float64{-75, 40, -73, 41}, nil)
	mustReadType(t, connNY, wsMsgTypeSubscribed)

	if err := pub.PublishFlightState(ctx, sampleFlightAt("sfflight", 37.0, -122.0)); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}

	got := readFlightUpdate(t, connSF, 3*time.Second)
	if got.State.ICAO24 != "sfflight" {
		t.Errorf("ICAO24 = %q, want sfflight", got.State.ICAO24)
	}

	assertNoMoreMessages(t, connNY, 300*time.Millisecond)
}

func TestWSUpdateViewportMidConnectionChangesFilter(t *testing.T) {
	wsURL, pub, _ := testWSEnv(t)
	ctx := context.Background()

	conn := dialWS(t, wsURL)
	subscribe(t, conn, []float64{-123, 36, -121, 38}, nil) // SF
	mustReadType(t, conn, wsMsgTypeSubscribed)

	subscribe(t, conn, []float64{-75, 40, -73, 41}, nil) // pan to NY
	time.Sleep(100 * time.Millisecond)                   // let the gateway's read loop apply it

	if err := pub.PublishFlightState(ctx, sampleFlightAt("ny", 40.7, -74.0)); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}
	got := readFlightUpdate(t, conn, 3*time.Second)
	if got.State.ICAO24 != "ny" {
		t.Errorf("ICAO24 = %q, want ny", got.State.ICAO24)
	}
}

func TestWSResumeReplaysMissedUpdatesAfterReconnect(t *testing.T) {
	wsURL, pub, _ := testWSEnv(t)
	ctx := context.Background()
	worldBBox := []float64{-180, -90, 180, 90}

	conn1 := dialWS(t, wsURL)
	subscribe(t, conn1, worldBBox, nil)
	ack := mustReadType(t, conn1, wsMsgTypeSubscribed)
	if ack.Seq != 0 {
		t.Fatalf("initial ack.Seq = %d, want 0 for an empty stream", ack.Seq)
	}

	if err := pub.PublishFlightState(ctx, sampleFlightAt("first", 0, 0)); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}
	got := readFlightUpdate(t, conn1, 3*time.Second)
	if got.State.ICAO24 != "first" {
		t.Fatalf("ICAO24 = %q, want first", got.State.ICAO24)
	}
	lastSeenSeq := got.Seq

	// Disconnect without reading any further messages.
	_ = conn1.Close(websocket.StatusNormalClosure, "")

	// Publish two more updates while this client is disconnected.
	if err := pub.PublishFlightState(ctx, sampleFlightAt("second", 0, 0)); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}
	if err := pub.PublishFlightState(ctx, sampleFlightAt("third", 0, 0)); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}

	// Reconnect and resume from the last sequence actually received.
	conn2 := dialWS(t, wsURL)
	subscribe(t, conn2, worldBBox, &lastSeenSeq)

	gotSecond := readFlightUpdate(t, conn2, 3*time.Second)
	if gotSecond.State.ICAO24 != "second" {
		t.Fatalf("first replayed message ICAO24 = %q, want second", gotSecond.State.ICAO24)
	}
	gotThird := readFlightUpdate(t, conn2, 3*time.Second)
	if gotThird.State.ICAO24 != "third" {
		t.Fatalf("second replayed message ICAO24 = %q, want third", gotThird.State.ICAO24)
	}

	ack2 := mustReadType(t, conn2, wsMsgTypeSubscribed)
	if ack2.Seq != gotThird.Seq {
		t.Fatalf("ack2.Seq = %d, want %d (the last replayed sequence)", ack2.Seq, gotThird.Seq)
	}

	// A new live update after reconnecting should still arrive normally.
	if err := pub.PublishFlightState(ctx, sampleFlightAt("fourth", 0, 0)); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}
	gotFourth := readFlightUpdate(t, conn2, 3*time.Second)
	if gotFourth.State.ICAO24 != "fourth" {
		t.Fatalf("ICAO24 = %q, want fourth", gotFourth.State.ICAO24)
	}
}

func TestWSResumeFailedFallsBackToLiveOnlyWhenSequenceNotRetained(t *testing.T) {
	wsURL, pub, js := testWSEnv(t)
	ctx := context.Background()
	worldBBox := []float64{-180, -90, 180, 90}

	for _, icao24 := range []string{"a", "b", "c"} {
		if err := pub.PublishFlightState(ctx, sampleFlightAt(icao24, 0, 0)); err != nil {
			t.Fatalf("PublishFlightState: %v", err)
		}
	}

	stream, err := js.Stream(ctx, natsutil.StreamFlightsUpdates)
	if err != nil {
		t.Fatalf("js.Stream: %v", err)
	}
	if err := stream.Purge(ctx, jetstream.WithPurgeKeep(1)); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	conn := dialWS(t, wsURL)
	oldSeq := uint64(1)
	subscribe(t, conn, worldBBox, &oldSeq)

	resumeFailed := readServerMessage(t, conn, 3*time.Second)
	if resumeFailed.Type != wsMsgTypeResumeFailed {
		t.Fatalf("message type = %q, want %q", resumeFailed.Type, wsMsgTypeResumeFailed)
	}
	if resumeFailed.Reason == "" {
		t.Error("resume_failed message has no reason")
	}

	mustReadType(t, conn, wsMsgTypeSubscribed)

	// The connection should still work for live updates after a failed resume.
	if err := pub.PublishFlightState(ctx, sampleFlightAt("live-after-resume-failed", 0, 0)); err != nil {
		t.Fatalf("PublishFlightState: %v", err)
	}
	got := readFlightUpdate(t, conn, 3*time.Second)
	if got.State.ICAO24 != "live-after-resume-failed" {
		t.Fatalf("ICAO24 = %q, want live-after-resume-failed", got.State.ICAO24)
	}
}

func TestWSRejectsNonSubscribeFirstMessage(t *testing.T) {
	wsURL, _, _ := testWSEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.Close(websocket.StatusInternalError, "")

	if err := wsjson.Write(ctx, conn, wsClientMessage{Type: "bogus"}); err != nil {
		t.Fatalf("wsjson.Write: %v", err)
	}

	var msg wsServerMessage
	err = wsjson.Read(ctx, conn, &msg)
	if err == nil {
		t.Fatal("want the gateway to close the connection for a non-subscribe first message, got a message instead")
	}
}

func TestWSRejectsInvalidBBox(t *testing.T) {
	wsURL, _, _ := testWSEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}
	defer conn.Close(websocket.StatusInternalError, "")

	if err := wsjson.Write(ctx, conn, wsClientMessage{Type: wsMsgTypeSubscribe, BBox: []float64{1, 2, 3}}); err != nil {
		t.Fatalf("wsjson.Write: %v", err)
	}

	var msg wsServerMessage
	if err := wsjson.Read(ctx, conn, &msg); err == nil {
		t.Fatal("want the gateway to close the connection for an invalid bbox, got a message instead")
	}
}
