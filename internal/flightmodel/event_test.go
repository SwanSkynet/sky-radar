package flightmodel

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestEventSchema(t *testing.T) {
	assertSchema(t, Event{}, []fieldSpec{
		{"ID", "id", "string"},
		{"Type", "type", "flightmodel.EventType"},
		{"ICAO24", "icao24", "string"},
		{"Severity", "severity", "flightmodel.EventSeverity"},
		{"OccurredAtUTC", "occurred_at_utc", "time.Time"},
		{"Detail", "detail", "json.RawMessage"},
	})
}

func TestEventJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	original := Event{
		ID:            "5f2c8b3a-0000-0000-0000-000000000000",
		Type:          EventTypeAltitudeDelta,
		ICAO24:        "a1b2c3",
		Severity:      EventSeverityNotable,
		OccurredAtUTC: now,
		Detail:        json.RawMessage(`{"delta_ft":3200}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	for _, key := range []string{"id", "type", "icao24", "severity", "occurred_at_utc", "detail"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing wire field %q in marshaled output", key)
		}
	}

	var roundTripped Event
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original, roundTripped) {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", roundTripped, original)
	}
}

func TestEventTypeAndSeverityValues(t *testing.T) {
	wantTypes := []EventType{
		EventTypeAltitudeDelta, EventTypeSpeedDelta, EventTypeStaleSignal,
		EventTypeGeofenceEnter, EventTypeGeofenceExit, EventTypeWatchlistMatch,
	}
	wantValues := []string{
		"altitude_delta", "speed_delta", "stale_signal",
		"geofence_enter", "geofence_exit", "watchlist_match",
	}
	for i, et := range wantTypes {
		if string(et) != wantValues[i] {
			t.Errorf("EventType %d: got %q, want %q", i, et, wantValues[i])
		}
	}

	wantSeverities := []EventSeverity{EventSeverityInfo, EventSeverityNotable, EventSeverityWarning}
	wantSeverityValues := []string{"info", "notable", "warning"}
	for i, es := range wantSeverities {
		if string(es) != wantSeverityValues[i] {
			t.Errorf("EventSeverity %d: got %q, want %q", i, es, wantSeverityValues[i])
		}
	}
}
