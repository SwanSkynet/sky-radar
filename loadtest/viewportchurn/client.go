package main

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// clientResult summarizes one simulated client's run, sent back to main
// for aggregation once the client disconnects.
type clientResult struct {
	id               int
	connectErr       error
	handshakeLatency time.Duration
	messagesReceived int
	resumeFailed     int
}

// clientConfig bundles the per-run settings every simulated client shares,
// so runClient's signature doesn't grow a parameter per flag.
type clientConfig struct {
	wsURL         string
	apiKey        string
	churnInterval time.Duration
	duration      time.Duration
	dialTimeout   time.Duration
}

// runClient simulates one browser tab: dial, subscribe to an initial
// viewport, then pan/zoom (re-subscribe with a new bbox) on a jittered
// interval while continuously draining flight_update messages, until
// duration elapses. Every per-message freshness sample is sent to
// samples — unbuffered backpressure here is deliberate, mirroring
// loadtest/ingestvolume's single-aggregator pattern, since samples is
// sized generously enough by the caller that a momentary stall doesn't
// block message draining in practice.
func runClient(ctx context.Context, id int, cfg clientConfig, samples chan<- float64) clientResult {
	res := clientResult{id: id}
	start := time.Now()

	dialCtx, cancelDial := context.WithTimeout(ctx, cfg.dialTimeout)
	defer cancelDial()

	var header http.Header
	if cfg.apiKey != "" {
		header = http.Header{"X-API-Key": []string{cfg.apiKey}}
	}
	conn, _, err := websocket.Dial(dialCtx, cfg.wsURL, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		res.connectErr = fmt.Errorf("dial: %w", err)
		return res
	}
	defer func() { _ = conn.CloseNow() }()

	clientCtx, cancel := context.WithTimeout(ctx, cfg.duration)
	defer cancel()

	view := newSimulatedViewport(int64(id) + time.Now().UnixNano())
	bbox := view.churn()
	if err := wsjson.Write(clientCtx, conn, wsClientMessage{Type: wsMsgTypeSubscribe, BBox: []float64{bbox.MinLon, bbox.MinLat, bbox.MaxLon, bbox.MaxLat}}); err != nil {
		res.connectErr = fmt.Errorf("write initial subscribe: %w", err)
		return res
	}

	var ack wsServerMessage
	if err := wsjson.Read(clientCtx, conn, &ack); err != nil {
		res.connectErr = fmt.Errorf("read subscribed ack: %w", err)
		return res
	}
	if ack.Type != wsMsgTypeSubscribed {
		res.connectErr = fmt.Errorf("expected %q ack, got %q", wsMsgTypeSubscribed, ack.Type)
		return res
	}
	res.handshakeLatency = time.Since(start)

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			var msg wsServerMessage
			if err := wsjson.Read(clientCtx, conn, &msg); err != nil {
				return
			}
			switch msg.Type {
			case wsMsgTypeFlightUpdate:
				res.messagesReceived++
				if msg.State != nil {
					select {
					case samples <- freshnessOf(msg.State).Seconds():
					default:
					}
				}
			case wsMsgTypeResumeFailed:
				res.resumeFailed++
			}
		}
	}()

	// jitteredChurnInterval re-randomizes per tick (rather than using a
	// fixed ticker) so a fleet of clients started together doesn't
	// re-subscribe in lockstep, which would understate steady-state
	// fan-out load by bunching every viewport update into the same instant.
	rng := rand.New(rand.NewSource(int64(id) + 1))
	timer := time.NewTimer(jitteredInterval(rng, cfg.churnInterval))
	defer timer.Stop()

	for {
		select {
		case <-clientCtx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "load test complete")
			<-readDone
			return res
		case <-timer.C:
			bbox := view.churn()
			_ = wsjson.Write(clientCtx, conn, wsClientMessage{Type: wsMsgTypeSubscribe, BBox: []float64{bbox.MinLon, bbox.MinLat, bbox.MaxLon, bbox.MaxLat}})
			timer.Reset(jitteredInterval(rng, cfg.churnInterval))
		}
	}
}

// jitteredInterval returns base scaled by a random factor in [0.5, 1.5),
// so churn ticks land at irregular real-world pan/zoom cadences instead
// of a perfectly periodic synthetic signal.
func jitteredInterval(rng *rand.Rand, base time.Duration) time.Duration {
	factor := 0.5 + rng.Float64()
	return time.Duration(float64(base) * factor)
}
