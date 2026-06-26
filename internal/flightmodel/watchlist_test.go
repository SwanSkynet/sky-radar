package flightmodel

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestWatchlistEntrySchema(t *testing.T) {
	assertSchema(t, WatchlistEntry{}, []fieldSpec{
		{"ID", "id", "string"},
		{"ICAO24", "icao24", "string"},
		{"Label", "label", "string"},
		{"CreatedBySession", "created_by_session", "string"},
		{"CreatedAt", "created_at", "time.Time"},
	})
}

func TestWatchlistEntryJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	original := WatchlistEntry{
		ID:               "5f2c8b3a-0000-0000-0000-000000000000",
		ICAO24:           "a1b2c3",
		Label:            "Friend's flight",
		CreatedBySession: "anon-session-1",
		CreatedAt:        now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	for _, key := range []string{"id", "icao24", "label", "created_by_session", "created_at"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing wire field %q in marshaled output", key)
		}
	}

	var roundTripped WatchlistEntry
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original, roundTripped) {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", roundTripped, original)
	}
}
