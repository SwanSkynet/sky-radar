package main

import (
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

// wsClientMessage and wsServerMessage mirror the wire envelopes defined in
// cmd/apigateway/ws_protocol.go. They are duplicated here deliberately
// rather than imported: this harness is an external client of the public
// /api/v1/ws contract, exactly like a real browser, and cmd/apigateway is
// package main (not importable) — duplicating the documented wire schema
// is the correct boundary, the same way a hand-written browser WebSocket
// client would, not a shortcut.
type wsClientMessage struct {
	Type          string    `json:"type"`
	BBox          []float64 `json:"bbox"`
	ResumeFromSeq *uint64   `json:"resume_from_seq,omitempty"`
}

type wsServerMessage struct {
	Type   string                   `json:"type"`
	Seq    uint64                   `json:"seq,omitempty"`
	State  *flightmodel.FlightState `json:"state,omitempty"`
	Reason string                   `json:"reason,omitempty"`
}

const (
	wsMsgTypeSubscribe    = "subscribe"
	wsMsgTypeSubscribed   = "subscribed"
	wsMsgTypeFlightUpdate = "flight_update"
	wsMsgTypeResumeFailed = "resume_failed"
)

// freshnessOf returns how long ago state was last seen at the source, the
// same freshness metric loadtest/ingestvolume measures, so both harnesses'
// reports are directly comparable against the master PRD's freshness SLO.
func freshnessOf(state *flightmodel.FlightState) time.Duration {
	if state == nil {
		return 0
	}
	return time.Since(state.LastSeenUTC)
}
