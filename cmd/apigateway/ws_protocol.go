package main

import (
	"fmt"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/geo"
)

// wsClientMessage is the envelope for every message a WebSocket client
// sends to the gateway. "subscribe" is the only message type today: a
// client sends it once on connect to register its viewport (optionally
// with ResumeFromSeq to ask for a resume replay), and may send it again
// later to update its viewport as the user pans/zooms the map — both
// cases are "(re)register this bbox", so one message type covers both per
// the constraint that this gateway stays a thin transport/filtering layer.
type wsClientMessage struct {
	Type          string    `json:"type"`
	BBox          []float64 `json:"bbox"`
	ResumeFromSeq *uint64   `json:"resume_from_seq,omitempty"`
}

const wsMsgTypeSubscribe = "subscribe"

// bboxFromSlice converts the wire [minLon,minLat,maxLon,maxLat] form into a
// validated geo.BBox.
func bboxFromSlice(v []float64) (geo.BBox, error) {
	if len(v) != 4 {
		return geo.BBox{}, fmt.Errorf("bbox must have exactly 4 values [minLon,minLat,maxLon,maxLat], got %d", len(v))
	}
	b := geo.BBox{MinLon: v[0], MinLat: v[1], MaxLon: v[2], MaxLat: v[3]}
	if err := b.Validate(); err != nil {
		return geo.BBox{}, err
	}
	return b, nil
}

// wsServerMessage is the envelope for every message the gateway sends to a
// WebSocket client.
type wsServerMessage struct {
	Type   string                   `json:"type"`
	Seq    uint64                   `json:"seq,omitempty"`
	State  *flightmodel.FlightState `json:"state,omitempty"`
	Reason string                   `json:"reason,omitempty"`
}

const (
	// wsMsgTypeSubscribed acknowledges a subscribe message, carrying the
	// sequence number the client should remember as its resume baseline.
	wsMsgTypeSubscribed = "subscribed"

	// wsMsgTypeFlightUpdate carries one in-viewport FlightState, tagged
	// with the FLIGHTS_UPDATES sequence number it was delivered at.
	wsMsgTypeFlightUpdate = "flight_update"

	// wsMsgTypeResumeFailed tells the client its requested resume_from_seq
	// has fallen out of the retained replay window, so it must fall back
	// to a full state reload (e.g. GET /flights) instead of trusting it
	// has a gapless update stream.
	wsMsgTypeResumeFailed = "resume_failed"
)
