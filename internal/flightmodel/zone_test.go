package flightmodel

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestZoneSchema(t *testing.T) {
	assertSchema(t, Zone{}, []fieldSpec{
		{"ID", "id", "string"},
		{"Name", "name", "string"},
		{"Polygon", "polygon", "flightmodel.GeoJSONPolygon"},
		{"CreatedBySession", "created_by_session", "string"},
		{"CreatedAt", "created_at", "time.Time"},
	})
}

func TestZoneJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	original := Zone{
		ID:   "5f2c8b3a-0000-0000-0000-000000000000",
		Name: "SFO Approach",
		Polygon: GeoJSONPolygon{
			Type: "Polygon",
			Coordinates: [][][]float64{
				{{-122.4, 37.6}, {-122.3, 37.6}, {-122.3, 37.7}, {-122.4, 37.6}},
			},
		},
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
	for _, key := range []string{"id", "name", "polygon", "created_by_session", "created_at"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing wire field %q in marshaled output", key)
		}
	}

	var roundTripped Zone
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original, roundTripped) {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", roundTripped, original)
	}
}
