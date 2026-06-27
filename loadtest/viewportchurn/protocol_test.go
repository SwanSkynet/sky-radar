package main

import (
	"encoding/json"
	"testing"
	"time"
)

// TestClientMessageWireShape locks in the exact JSON field names
// cmd/apigateway/ws_protocol.go's wsClientMessage expects, so a future
// drift in that contract (e.g. a renamed field) breaks this test instead
// of silently producing a harness that always fails to subscribe.
func TestClientMessageWireShape(t *testing.T) {
	seq := uint64(42)
	msg := wsClientMessage{Type: wsMsgTypeSubscribe, BBox: []float64{-1, -2, 3, 4}, ResumeFromSeq: &seq}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, field := range []string{"type", "bbox", "resume_from_seq"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("encoded message missing expected field %q: %s", field, data)
		}
	}
	if decoded["type"] != wsMsgTypeSubscribe {
		t.Errorf("type = %v, want %q", decoded["type"], wsMsgTypeSubscribe)
	}
}

// TestServerMessageDecodesFlightUpdate verifies a server flight_update
// message shaped like cmd/apigateway/ws_protocol.go's wsServerMessage
// decodes into a usable flightmodel.FlightState, which freshnessOf then
// measures against.
func TestServerMessageDecodesFlightUpdate(t *testing.T) {
	lastSeen := time.Now().Add(-3 * time.Second).UTC()
	raw := `{"type":"flight_update","seq":7,"state":{"icao24":"abc123","lat":1.5,"lon":2.5,"last_seen_utc":"` + lastSeen.Format(time.RFC3339Nano) + `"}}`

	var msg wsServerMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Type != wsMsgTypeFlightUpdate || msg.Seq != 7 {
		t.Fatalf("msg = %+v, want type=flight_update seq=7", msg)
	}
	if msg.State == nil || msg.State.ICAO24 != "abc123" {
		t.Fatalf("State = %+v, want icao24=abc123", msg.State)
	}

	freshness := freshnessOf(msg.State)
	if freshness < 2*time.Second || freshness > 10*time.Second {
		t.Fatalf("freshnessOf = %v, want roughly 3s", freshness)
	}
}

func TestFreshnessOfNilState(t *testing.T) {
	if got := freshnessOf(nil); got != 0 {
		t.Fatalf("freshnessOf(nil) = %v, want 0", got)
	}
}
