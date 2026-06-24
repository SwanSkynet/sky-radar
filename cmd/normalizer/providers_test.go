package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
)

func TestParseOpenSkyStateVector(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	vector := []any{
		"3c6444", "DLH123 ", "Germany", 1458564118, 1458564118,
		9.9935, 53.5553, 11300.8, false, 222.5, 40.7, 0.0,
		[]int{12345}, 11100.0, "7000", false, 2, 0,
	}
	payload, err := json.Marshal(vector)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}

	got, err := parseOpenSkyStateVector(payload, now)
	if err != nil {
		t.Fatalf("parseOpenSkyStateVector: %v", err)
	}

	if got.ICAO24 != "3c6444" {
		t.Errorf("ICAO24 = %q, want 3c6444", got.ICAO24)
	}
	if got.Callsign == nil || *got.Callsign != "DLH123" {
		t.Errorf("Callsign = %v, want DLH123 (trimmed)", got.Callsign)
	}
	if got.Lat != 53.5553 || got.Lon != 9.9935 {
		t.Errorf("Lat/Lon = %v/%v, want 53.5553/9.9935", got.Lat, got.Lon)
	}
	if got.AltitudeBaroFt == nil || *got.AltitudeBaroFt != 37076 {
		t.Errorf("AltitudeBaroFt = %v, want 37076 (11300.8m converted)", got.AltitudeBaroFt)
	}
	if got.GroundSpeedKt == nil || int(*got.GroundSpeedKt) != 432 {
		t.Errorf("GroundSpeedKt = %v, want ~432 (222.5 m/s converted)", got.GroundSpeedKt)
	}
	if got.OnGround {
		t.Error("OnGround = true, want false")
	}
	if got.PositionQuality != flightmodel.PositionQualityMLAT {
		t.Errorf("PositionQuality = %v, want mlat (position_source=2)", got.PositionQuality)
	}
}

func TestParseOpenSkyStateVectorTooShort(t *testing.T) {
	payload, _ := json.Marshal([]any{"3c6444"})
	if _, err := parseOpenSkyStateVector(payload, time.Now()); err == nil {
		t.Fatal("want error for short state vector, got nil")
	}
}

func TestParseOpenSkyADSBSource(t *testing.T) {
	vector := []any{
		"abc123", nil, "Testland", nil, nil,
		1.0, 2.0, 1000.0, false, 100.0, 90.0, nil,
		nil, nil, nil, false, 0, 0,
	}
	payload, _ := json.Marshal(vector)

	got, err := parseOpenSkyStateVector(payload, time.Now())
	if err != nil {
		t.Fatalf("parseOpenSkyStateVector: %v", err)
	}
	if got.PositionQuality != flightmodel.PositionQualityADSB {
		t.Errorf("PositionQuality = %v, want adsb (position_source=0)", got.PositionQuality)
	}
	if got.Callsign != nil {
		t.Errorf("Callsign = %v, want nil for null field", got.Callsign)
	}
}

func TestParseReadsbAircraftNumericAltitude(t *testing.T) {
	payload := []byte(`{"hex":"45211e","flight":"CFG846 ","r":"LZ-LAJ","alt_baro":37000,"gs":496,"track":113.55,"lat":43.261414,"lon":29.636404,"squawk":"7665","type":"adsb_icao"}`)

	got, err := parseReadsbAircraft("airplanes.live", payload, time.Now())
	if err != nil {
		t.Fatalf("parseReadsbAircraft: %v", err)
	}
	if got.ICAO24 != "45211e" {
		t.Errorf("ICAO24 = %q, want 45211e", got.ICAO24)
	}
	if got.Callsign == nil || *got.Callsign != "CFG846" {
		t.Errorf("Callsign = %v, want CFG846 (trimmed)", got.Callsign)
	}
	if got.Registration == nil || *got.Registration != "LZ-LAJ" {
		t.Errorf("Registration = %v, want LZ-LAJ", got.Registration)
	}
	if got.AltitudeBaroFt == nil || *got.AltitudeBaroFt != 37000 {
		t.Errorf("AltitudeBaroFt = %v, want 37000", got.AltitudeBaroFt)
	}
	if got.OnGround {
		t.Error("OnGround = true, want false")
	}
	if got.PositionQuality != flightmodel.PositionQualityADSB {
		t.Errorf("PositionQuality = %v, want adsb", got.PositionQuality)
	}
}

func TestParseReadsbAircraftGroundString(t *testing.T) {
	payload := []byte(`{"hex":"45211e","alt_baro":"ground","type":"adsb_icao"}`)

	got, err := parseReadsbAircraft("adsb.lol", payload, time.Now())
	if err != nil {
		t.Fatalf("parseReadsbAircraft: %v", err)
	}
	if got.AltitudeBaroFt != nil {
		t.Errorf("AltitudeBaroFt = %v, want nil when alt_baro is \"ground\"", got.AltitudeBaroFt)
	}
	if !got.OnGround {
		t.Error("OnGround = false, want true")
	}
}

func TestParseReadsbAircraftMLATType(t *testing.T) {
	payload := []byte(`{"hex":"45211e","alt_baro":10000,"type":"mlat"}`)

	got, err := parseReadsbAircraft("airplanes.live", payload, time.Now())
	if err != nil {
		t.Fatalf("parseReadsbAircraft: %v", err)
	}
	if got.PositionQuality != flightmodel.PositionQualityMLAT {
		t.Errorf("PositionQuality = %v, want mlat", got.PositionQuality)
	}
}

func TestParseReadsbAircraftMissingTypeDefaultsToADSB(t *testing.T) {
	payload := []byte(`{"hex":"45211e","alt_baro":10000}`)

	got, err := parseReadsbAircraft("adsb.lol", payload, time.Now())
	if err != nil {
		t.Fatalf("parseReadsbAircraft: %v", err)
	}
	if got.PositionQuality != flightmodel.PositionQualityADSB {
		t.Errorf("PositionQuality = %v, want adsb (default when type is absent)", got.PositionQuality)
	}
}

func TestParseReadsbAircraftMissingHexReturnsError(t *testing.T) {
	payload := []byte(`{"alt_baro":10000}`)
	if _, err := parseReadsbAircraft("adsb.lol", payload, time.Now()); err == nil {
		t.Fatal("want error for missing hex, got nil")
	}
}

func TestParseReadsbAircraftInvalidAltBaroStringReturnsError(t *testing.T) {
	payload := []byte(`{"hex":"45211e","alt_baro":"airborne"}`)
	if _, err := parseReadsbAircraft("adsb.lol", payload, time.Now()); err == nil {
		t.Fatal("want error for unrecognized alt_baro string, got nil")
	}
}

func TestParseRawStateUnknownProviderReturnsError(t *testing.T) {
	raw := sourceadapter.RawState{Provider: "unknown-provider", ICAO24: "a1b2c3", Payload: json.RawMessage(`{}`)}
	if _, err := ParseRawState(raw); err == nil {
		t.Fatal("want error for unknown provider, got nil")
	}
}
