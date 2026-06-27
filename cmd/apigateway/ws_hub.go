package main

import (
	"context"
	"sync"

	"github.com/SwanSkynet/sky-radar/internal/geo"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/SwanSkynet/sky-radar/internal/otelutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// otelMeter and the instruments below are created against otel's global,
// delegating Meter: they delegate to a no-op implementation until main
// calls otelutil.Init. wsActiveConnections and wsMessagesSent back the
// master PRD's WebSocket fan-out SLO ("per-connection bandwidth bounded by
// viewport regardless of global aircraft count") — the bound itself is
// enforced by wsHub.broadcast's per-client viewport filter and verified by
// the manual small-bbox-vs-large-bbox test in
// docs/prd/phase-2-realtime-systems.md; these two counters give a live
// view of connection/message volume as a sanity signal on the dashboard.
var (
	otelMeter           = otel.Meter(serviceName)
	wsActiveConnections = otelutil.MustInt64UpDownCounter(otelMeter, "skyradar.ws.active_connections",
		metric.WithDescription("Currently connected WebSocket clients"))
	wsMessagesSent = otelutil.MustInt64Counter(otelMeter, "skyradar.ws.messages_sent",
		metric.WithDescription("Flight update messages delivered to WebSocket clients"))
)

// wsSendBufferSize bounds how many undelivered flight updates a single
// client's outgoing queue holds before the hub starts dropping messages
// for that client rather than blocking the shared broadcast loop on one
// slow connection. A dropped message is recoverable: the client can
// reconnect and resume from the last sequence it actually received.
const wsSendBufferSize = 64

// wsClient is one connected WebSocket client's registration with the hub:
// its current viewport filter and the channel the hub pushes in-viewport
// updates onto.
type wsClient struct {
	send chan natsutil.FlightStateMessage

	mu   sync.RWMutex
	bbox geo.BBox
}

func newWSClient() *wsClient {
	return &wsClient{send: make(chan natsutil.FlightStateMessage, wsSendBufferSize)}
}

// BBox returns c's currently registered viewport.
func (c *wsClient) BBox() geo.BBox {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.bbox
}

// SetBBox updates c's registered viewport, e.g. when the client pans or
// zooms the map.
func (c *wsClient) SetBBox(b geo.BBox) {
	c.mu.Lock()
	c.bbox = b
	c.mu.Unlock()
}

// wsHub fans out flights.updates messages to every registered client,
// filtering each by that client's own registered viewport — this is the
// "bounded per-connection bandwidth regardless of global traffic"
// mechanism described in docs/architecture/system-architecture.md.
type wsHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

func newWSHub() *wsHub {
	return &wsHub{clients: make(map[*wsClient]struct{})}
}

// register adds c to the broadcast set. Callers must unregister when the
// client disconnects.
func (h *wsHub) register(c *wsClient) {
	h.mu.Lock()
	_, exists := h.clients[c]
	if !exists {
		h.clients[c] = struct{}{}
	}
	h.mu.Unlock()
	if !exists {
		wsActiveConnections.Add(context.Background(), 1)
	}
}

// unregister removes c from the broadcast set.
func (h *wsHub) unregister(c *wsClient) {
	h.mu.Lock()
	_, exists := h.clients[c]
	if exists {
		delete(h.clients, c)
	}
	h.mu.Unlock()
	if exists {
		wsActiveConnections.Add(context.Background(), -1)
	}
}

// broadcast delivers msg to every registered client whose viewport
// contains the aircraft's position. A client whose outgoing buffer is
// already full has msg dropped for it rather than blocking delivery to
// every other client.
func (h *wsHub) broadcast(msg natsutil.FlightStateMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if !c.BBox().Contains(msg.State.Lat, msg.State.Lon) {
			continue
		}
		select {
		case c.send <- msg:
		default:
		}
	}
}

// clientCount reports how many clients are currently registered, for
// tests and observability.
func (h *wsHub) clientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}
