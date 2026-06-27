package flightmodel

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// fieldSpec pins a struct field's name, JSON wire tag, and Go type so a
// test can fail loudly if the canonical schema drifts from
// docs/architecture/data-model.md instead of drifting silently.
type fieldSpec struct {
	name string
	json string
	typ  string
}

func assertSchema(t *testing.T, v any, want []fieldSpec) {
	t.Helper()
	rt := reflect.TypeOf(v)
	if rt.NumField() != len(want) {
		t.Fatalf("%s: got %d fields, want %d", rt.Name(), rt.NumField(), len(want))
	}
	for i, spec := range want {
		f := rt.Field(i)
		if f.Name != spec.name {
			t.Errorf("field %d: got name %q, want %q", i, f.Name, spec.name)
		}
		if tag := f.Tag.Get("json"); tag != spec.json {
			t.Errorf("field %s: got json tag %q, want %q", f.Name, tag, spec.json)
		}
		if got := f.Type.String(); got != spec.typ {
			t.Errorf("field %s: got type %q, want %q", f.Name, got, spec.typ)
		}
	}
}

func TestFlightStateSchema(t *testing.T) {
	assertSchema(t, FlightState{}, []fieldSpec{
		{"ICAO24", "icao24", "string"},
		{"Callsign", "callsign", "*string"},
		{"Registration", "registration", "*string"},
		{"Lat", "lat", "float64"},
		{"Lon", "lon", "float64"},
		{"AltitudeBaroFt", "altitude_baro_ft", "*int"},
		{"AltitudeGeoFt", "altitude_geo_ft", "*int"},
		{"GroundSpeedKt", "ground_speed_kt", "*float64"},
		{"VerticalRateFpm", "vertical_rate_fpm", "*float64"},
		{"HeadingDeg", "heading_deg", "*float64"},
		{"OnGround", "on_ground", "bool"},
		{"Squawk", "squawk", "*string"},
		{"Sources", "sources", "[]string"},
		{"PositionQuality", "position_quality", "flightmodel.PositionQuality"},
		{"LastSeenUTC", "last_seen_utc", "time.Time"},
		{"Stale", "stale", "bool"},
		{"AircraftType", "aircraft_type", "*string"},
		{"EmitterCategory", "emitter_category", "*string"},
		{"Military", "military", "bool"},
		{"IconClass", "icon_class", "*string"},
	})
}

func TestFlightStateJSONRoundTrip(t *testing.T) {
	callsign := "UAL123"
	altBaro := 35000
	gs := 420.5
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	original := FlightState{
		ICAO24:          "a1b2c3",
		Callsign:        &callsign,
		Lat:             37.6188,
		Lon:             -122.3758,
		AltitudeBaroFt:  &altBaro,
		GroundSpeedKt:   &gs,
		OnGround:        false,
		Sources:         []string{"opensky", "adsblol"},
		PositionQuality: PositionQualityADSB,
		LastSeenUTC:     now,
		Stale:           false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal into map: %v", err)
	}
	for _, key := range []string{
		"icao24", "callsign", "registration", "lat", "lon",
		"altitude_baro_ft", "altitude_geo_ft", "ground_speed_kt",
		"vertical_rate_fpm", "heading_deg", "on_ground", "squawk",
		"sources", "position_quality", "last_seen_utc", "stale",
		"aircraft_type", "emitter_category", "military", "icon_class",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("missing wire field %q in marshaled output", key)
		}
	}

	var roundTripped FlightState
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original, roundTripped) {
		t.Errorf("round trip mismatch:\n got  %+v\n want %+v", roundTripped, original)
	}
}
