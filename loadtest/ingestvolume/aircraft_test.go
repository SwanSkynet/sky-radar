package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNewAircraftFleetDeterministic(t *testing.T) {
	a := newAircraftFleet(50, 0.3, 42)
	b := newAircraftFleet(50, 0.3, 42)

	if len(a) != 50 || len(b) != 50 {
		t.Fatalf("len(a)=%d len(b)=%d, want 50", len(a), len(b))
	}
	for i := range a {
		if a[i].icao24 != b[i].icao24 || a[i].lat != b[i].lat || a[i].lon != b[i].lon {
			t.Fatalf("aircraft %d differs between same-seed fleets: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func TestNewAircraftFleetICAO24IsSyntheticBlock(t *testing.T) {
	fleet := newAircraftFleet(10, 0, 1)
	seen := make(map[string]bool)
	for _, a := range fleet {
		if !strings.HasPrefix(a.icao24, syntheticICAO24Block) {
			t.Fatalf("icao24 %q does not start with synthetic block %q", a.icao24, syntheticICAO24Block)
		}
		if seen[a.icao24] {
			t.Fatalf("duplicate icao24 %q in fleet", a.icao24)
		}
		seen[a.icao24] = true
	}
}

func TestStepKeepsCoordinatesInRange(t *testing.T) {
	a := &aircraft{lat: 84, lon: 179, groundSpdKt: 500, headingDeg: 45}
	for i := 0; i < 100; i++ {
		a.step(15 * time.Second)
		if a.lat < -90 || a.lat > 90 {
			t.Fatalf("lat out of range after step %d: %v", i, a.lat)
		}
		if a.lon < -180 || a.lon >= 180 {
			t.Fatalf("lon out of range after step %d: %v", i, a.lon)
		}
	}
}

func TestStepMovesPosition(t *testing.T) {
	a := &aircraft{lat: 0, lon: 0, groundSpdKt: 480, headingDeg: 0}
	a.step(15 * time.Second)
	if a.lat == 0 && a.lon == 0 {
		t.Fatal("step did not move the aircraft at all")
	}
}

// readsbAircraftLike mirrors the subset of cmd/normalizer/providers.go's
// readsbAircraft this test cares about, kept local since that type lives
// in package main of a different binary and isn't importable here. This
// is a wire-shape contract check, not a behavioral one: it fails loudly
// if readsbPayload ever stops emitting a field the real normalizer parser
// requires.
type readsbAircraftLike struct {
	Hex     string          `json:"hex"`
	Flight  *string         `json:"flight"`
	AltBaro json.RawMessage `json:"alt_baro"`
	GS      *float64        `json:"gs"`
	Lat     *float64        `json:"lat"`
	Lon     *float64        `json:"lon"`
}

func TestReadsbPayloadShape(t *testing.T) {
	a := &aircraft{icao24: "fff001", callsign: "LT0001", altitudeFt: 35000, groundSpdKt: 400, lat: 51.5, lon: -0.1}
	var decoded readsbAircraftLike
	if err := json.Unmarshal(a.readsbPayload(), &decoded); err != nil {
		t.Fatalf("readsbPayload did not decode as a readsb aircraft: %v", err)
	}
	if decoded.Hex != "fff001" {
		t.Errorf("hex = %q, want fff001", decoded.Hex)
	}
	if decoded.Lat == nil || *decoded.Lat != 51.5 {
		t.Errorf("lat = %v, want 51.5", decoded.Lat)
	}
	if decoded.GS == nil || *decoded.GS != 400 {
		t.Errorf("gs = %v, want 400", decoded.GS)
	}
	var altFt float64
	if err := json.Unmarshal(decoded.AltBaro, &altFt); err != nil {
		t.Fatalf("alt_baro did not decode as a number: %v", err)
	}
	if altFt != 35000 {
		t.Errorf("alt_baro = %v, want 35000", altFt)
	}
}

func TestOpenSkyPayloadShape(t *testing.T) {
	a := &aircraft{icao24: "fff002", callsign: "LT0002", altitudeFt: 10000, groundSpdKt: 300, lat: 40.0, lon: -74.0}
	now := time.Now()
	var fields []json.RawMessage
	if err := json.Unmarshal(a.openSkyPayload(now), &fields); err != nil {
		t.Fatalf("openSkyPayload did not decode as an array: %v", err)
	}
	const minFields = 17
	if len(fields) < minFields {
		t.Fatalf("openSkyPayload has %d fields, want at least %d (cmd/normalizer/providers.go requires this)", len(fields), minFields)
	}

	var icao24 string
	if err := json.Unmarshal(fields[0], &icao24); err != nil || icao24 != "fff002" {
		t.Errorf("field[0] icao24 = %q (err=%v), want fff002", icao24, err)
	}
	var lon, lat float64
	if err := json.Unmarshal(fields[5], &lon); err != nil || lon != -74.0 {
		t.Errorf("field[5] longitude = %v (err=%v), want -74.0", lon, err)
	}
	if err := json.Unmarshal(fields[6], &lat); err != nil || lat != 40.0 {
		t.Errorf("field[6] latitude = %v (err=%v), want 40.0", lat, err)
	}
}

func TestRawStatesMultiSourceProducesTwoReports(t *testing.T) {
	a := &aircraft{icao24: "fff003", providers: []string{"adsb.lol", "airplanes.live"}}
	states := a.rawStates(time.Now())
	if len(states) != 2 {
		t.Fatalf("len(states) = %d, want 2", len(states))
	}
	providers := map[string]bool{}
	for _, s := range states {
		if s.ICAO24 != "fff003" {
			t.Errorf("state icao24 = %q, want fff003", s.ICAO24)
		}
		providers[s.Provider] = true
	}
	if !providers["adsb.lol"] || !providers["airplanes.live"] {
		t.Fatalf("providers = %v, want both adsb.lol and airplanes.live", providers)
	}
}

func TestRawStatesProviderDispatchesCorrectPayloadShape(t *testing.T) {
	a := &aircraft{icao24: "fff004", providers: []string{"opensky"}, lat: 1, lon: 2}
	states := a.rawStates(time.Now())
	if len(states) != 1 {
		t.Fatalf("len(states) = %d, want 1", len(states))
	}
	var fields []json.RawMessage
	if err := json.Unmarshal(states[0].Payload, &fields); err != nil {
		t.Fatalf("opensky-provider raw state payload is not an array: %v", err)
	}
}
