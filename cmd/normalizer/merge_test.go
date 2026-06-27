package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
)

func readsbRaw(t *testing.T, provider, hex string, fetchedAt time.Time, fields map[string]any) sourceadapter.RawState {
	t.Helper()
	fields["hex"] = hex
	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return sourceadapter.RawState{
		Provider:  provider,
		ICAO24:    hex,
		FetchedAt: fetchedAt,
		Payload:   payload,
	}
}

func openSkyRaw(t *testing.T, icao24 string, fetchedAt time.Time, positionSource int, lat, lon float64) sourceadapter.RawState {
	t.Helper()
	vector := []any{
		icao24, "TEST123 ", "Testland", 0, 0,
		lon, lat, 1000.0, false, 100.0, 90.0, 0.0,
		nil, 1000.0, "1200", false, positionSource, 0,
	}
	payload, err := json.Marshal(vector)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return sourceadapter.RawState{
		Provider:  "opensky",
		ICAO24:    icao24,
		FetchedAt: fetchedAt,
		Payload:   payload,
	}
}

func TestMergeSingleSource(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	raw := readsbRaw(t, "airplanes.live", "a1b2c3", now, map[string]any{
		"flight":   "UAL123",
		"lat":      37.0,
		"lon":      -122.0,
		"alt_baro": 35000,
		"gs":       450,
		"type":     "adsb_icao",
	})

	got, err := Merge("a1b2c3", []sourceadapter.RawState{raw})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if want := []string{"airplanes.live"}; !equalStrings(got.Sources, want) {
		t.Errorf("Sources = %v, want %v", got.Sources, want)
	}
	if got.PositionQuality != flightmodel.PositionQualityADSB {
		t.Errorf("PositionQuality = %v, want adsb", got.PositionQuality)
	}
	if got.Lat != 37.0 || got.Lon != -122.0 {
		t.Errorf("Lat/Lon = %v/%v, want 37.0/-122.0", got.Lat, got.Lon)
	}
	if got.Callsign == nil || *got.Callsign != "UAL123" {
		t.Errorf("Callsign = %v, want UAL123", got.Callsign)
	}
	if !got.LastSeenUTC.Equal(now) {
		t.Errorf("LastSeenUTC = %v, want %v", got.LastSeenUTC, now)
	}
}

func TestMergeMultiSourceAgreement(t *testing.T) {
	t0 := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	adsblol := readsbRaw(t, "adsb.lol", "a1b2c3", t0, map[string]any{
		"flight":   "UAL123",
		"lat":      37.0,
		"lon":      -122.0,
		"alt_baro": 35000,
		"type":     "adsb_icao",
	})
	airplaneslive := readsbRaw(t, "airplanes.live", "a1b2c3", t0.Add(1*time.Second), map[string]any{
		"flight":   "UAL123",
		"lat":      37.0,
		"lon":      -122.0,
		"alt_baro": 35000,
		"type":     "adsb_icao",
	})

	got, err := Merge("a1b2c3", []sourceadapter.RawState{adsblol, airplaneslive})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	want := []string{"adsb.lol", "airplanes.live"}
	if !equalStrings(got.Sources, want) {
		t.Errorf("Sources = %v, want %v (both providers credited even though only one wins)", got.Sources, want)
	}
	if got.Lat != 37.0 || got.Lon != -122.0 {
		t.Errorf("Lat/Lon = %v/%v, want 37.0/-122.0 (providers agree)", got.Lat, got.Lon)
	}
	if got.AltitudeBaroFt == nil || *got.AltitudeBaroFt != 35000 {
		t.Errorf("AltitudeBaroFt = %v, want 35000", got.AltitudeBaroFt)
	}
}

func TestMergeFreshnessTiebreak(t *testing.T) {
	t0 := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	// adsb.lol is higher quality (defaults to adsb with no "type") but
	// stale by a full minute -- well outside freshnessTieWindow, so the
	// more recent, lower-quality mlat report from airplanes.live must
	// win outright on freshness alone.
	older := readsbRaw(t, "adsb.lol", "a1b2c3", t0, map[string]any{
		"lat": 10.0, "lon": 10.0, "alt_baro": 10000,
	})
	newer := readsbRaw(t, "airplanes.live", "a1b2c3", t0.Add(time.Minute), map[string]any{
		"lat": 20.0, "lon": 20.0, "alt_baro": 20000, "type": "mlat",
	})

	got, err := Merge("a1b2c3", []sourceadapter.RawState{older, newer})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if got.Lat != 20.0 || got.Lon != 20.0 {
		t.Errorf("Lat/Lon = %v/%v, want the newer (mlat) report's 20.0/20.0", got.Lat, got.Lon)
	}
	if got.PositionQuality != flightmodel.PositionQualityMLAT {
		t.Errorf("PositionQuality = %v, want mlat (freshness should win over quality outside the tie window)", got.PositionQuality)
	}
	if !got.LastSeenUTC.Equal(t0.Add(time.Minute)) {
		t.Errorf("LastSeenUTC = %v, want %v", got.LastSeenUTC, t0.Add(time.Minute))
	}
}

func TestMergeQualityTiebreak(t *testing.T) {
	t0 := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	// airplanes.live's mlat report is technically a few seconds more
	// recent, but well within freshnessTieWindow of adsb.lol's adsb
	// report, so quality (adsb > mlat) must decide the winner, not the
	// raw timestamp comparison.
	adsb := readsbRaw(t, "adsb.lol", "a1b2c3", t0, map[string]any{
		"lat": 10.0, "lon": 10.0, "alt_baro": 10000,
	})
	mlat := readsbRaw(t, "airplanes.live", "a1b2c3", t0.Add(5*time.Second), map[string]any{
		"lat": 20.0, "lon": 20.0, "alt_baro": 20000, "type": "mlat",
	})

	got, err := Merge("a1b2c3", []sourceadapter.RawState{adsb, mlat})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if got.Lat != 10.0 || got.Lon != 10.0 {
		t.Errorf("Lat/Lon = %v/%v, want the adsb report's 10.0/10.0 (quality tie-break)", got.Lat, got.Lon)
	}
	if got.PositionQuality != flightmodel.PositionQualityADSB {
		t.Errorf("PositionQuality = %v, want adsb", got.PositionQuality)
	}
	if want := []string{"adsb.lol", "airplanes.live"}; !equalStrings(got.Sources, want) {
		t.Errorf("Sources = %v, want %v", got.Sources, want)
	}
}

func TestMergeTypeSurvivesOpenSkyPositionalWinner(t *testing.T) {
	t0 := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	// OpenSky is the freshest positional report (and thus the positional
	// winner), but carries no type. adsb.lol's type designator must still
	// flow through, and the icon class must be derived from it.
	opensky := openSkyRaw(t, "a1b2c3", t0.Add(3*time.Second), 0, 30.0, 30.0)
	adsblol := readsbRaw(t, "adsb.lol", "a1b2c3", t0, map[string]any{
		"lat": 30.1, "lon": 30.1, "t": "B738", "category": "A3", "type": "adsb_icao",
	})

	got, err := Merge("a1b2c3", []sourceadapter.RawState{opensky, adsblol})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// Positional fields come from OpenSky (the freshest report)...
	if got.Lat != 30.0 || got.Lon != 30.0 {
		t.Errorf("Lat/Lon = %v/%v, want OpenSky's 30.0/30.0", got.Lat, got.Lon)
	}
	// ...but the type fields come from adsb.lol.
	if got.AircraftType == nil || *got.AircraftType != "B738" {
		t.Errorf("AircraftType = %v, want B738 (from adsb.lol)", got.AircraftType)
	}
	if got.IconClass == nil || *got.IconClass != "commercial_jet" {
		t.Errorf("IconClass = %v, want commercial_jet", got.IconClass)
	}
}

func TestMergeMilitaryFlagClassifies(t *testing.T) {
	t0 := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	raw := readsbRaw(t, "airplanes.live", "ae1234", t0, map[string]any{
		"lat": 34.0, "lon": -118.0, "t": "KC135", "dbFlags": 1, "type": "adsb_icao",
	})

	got, err := Merge("ae1234", []sourceadapter.RawState{raw})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !got.Military {
		t.Error("Military = false, want true")
	}
	if got.IconClass == nil || *got.IconClass != "tanker" {
		t.Errorf("IconClass = %v, want tanker", got.IconClass)
	}
}

func TestMergeOpenSkyOnlyHasNoIconClass(t *testing.T) {
	t0 := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	raw := openSkyRaw(t, "a1b2c3", t0, 0, 30.0, 30.0)

	got, err := Merge("a1b2c3", []sourceadapter.RawState{raw})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if got.AircraftType != nil {
		t.Errorf("AircraftType = %v, want nil for OpenSky-only", got.AircraftType)
	}
	if got.IconClass != nil {
		t.Errorf("IconClass = %v, want nil for OpenSky-only", got.IconClass)
	}
}

func TestMergeThreeProvidersCreditsAllSources(t *testing.T) {
	t0 := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	opensky := openSkyRaw(t, "a1b2c3", t0, 0, 30.0, 30.0)
	adsblol := readsbRaw(t, "adsb.lol", "a1b2c3", t0.Add(1*time.Second), map[string]any{"lat": 30.1, "lon": 30.1})
	airplaneslive := readsbRaw(t, "airplanes.live", "a1b2c3", t0.Add(2*time.Second), map[string]any{"lat": 30.2, "lon": 30.2, "type": "adsb_icao"})

	got, err := Merge("a1b2c3", []sourceadapter.RawState{opensky, adsblol, airplaneslive})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	want := []string{"adsb.lol", "airplanes.live", "opensky"}
	if !equalStrings(got.Sources, want) {
		t.Errorf("Sources = %v, want %v", got.Sources, want)
	}
}

func TestMergeDropsReportWhosePayloadICAO24DoesNotMatchKey(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	// The RawState's ICAO24 (the merge key, e.g. derived from the Redis
	// key it was read from) claims "a1b2c3", but the payload itself
	// reports a different aircraft's hex. This must be dropped rather
	// than letting the wrong aircraft's fields populate a1b2c3's state.
	mismatched := readsbRaw(t, "airplanes.live", "ffffff", now, map[string]any{"lat": 99.0, "lon": 99.0})
	mismatched.ICAO24 = "a1b2c3"

	good := readsbRaw(t, "adsb.lol", "a1b2c3", now, map[string]any{"lat": 1.0, "lon": 2.0})

	got, err := Merge("a1b2c3", []sourceadapter.RawState{mismatched, good})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if want := []string{"adsb.lol"}; !equalStrings(got.Sources, want) {
		t.Errorf("Sources = %v, want %v (mismatched-ICAO24 report dropped)", got.Sources, want)
	}
	if got.Lat != 1.0 || got.Lon != 2.0 {
		t.Errorf("Lat/Lon = %v/%v, want the matching report's 1.0/2.0", got.Lat, got.Lon)
	}
}

func TestMergeAllMismatchedICAO24ReturnsError(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	mismatched := readsbRaw(t, "airplanes.live", "ffffff", now, map[string]any{"lat": 99.0, "lon": 99.0})
	mismatched.ICAO24 = "a1b2c3"

	if _, err := Merge("a1b2c3", []sourceadapter.RawState{mismatched}); err == nil {
		t.Fatal("Merge() with only a mismatched-ICAO24 report: want error, got nil")
	}
}

func TestMergeNoReportsReturnsError(t *testing.T) {
	if _, err := Merge("a1b2c3", nil); err == nil {
		t.Fatal("Merge() with no reports: want error, got nil")
	}
}

func TestMergeAllReportsMalformedReturnsError(t *testing.T) {
	bad := sourceadapter.RawState{Provider: "airplanes.live", ICAO24: "a1b2c3", Payload: json.RawMessage(`not json`)}
	if _, err := Merge("a1b2c3", []sourceadapter.RawState{bad}); err == nil {
		t.Fatal("Merge() with only malformed reports: want error, got nil")
	}
}

func TestMergeSkipsMalformedProviderButMergesOthers(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	bad := sourceadapter.RawState{Provider: "opensky", ICAO24: "a1b2c3", Payload: json.RawMessage(`[]`)}
	good := readsbRaw(t, "airplanes.live", "a1b2c3", now, map[string]any{"lat": 1.0, "lon": 2.0})

	got, err := Merge("a1b2c3", []sourceadapter.RawState{bad, good})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if want := []string{"airplanes.live"}; !equalStrings(got.Sources, want) {
		t.Errorf("Sources = %v, want %v (malformed opensky report dropped)", got.Sources, want)
	}
}

func TestMergeAllGroupsByICAO24(t *testing.T) {
	t0 := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	raws := []sourceadapter.RawState{
		readsbRaw(t, "adsb.lol", "aaaaaa", t0, map[string]any{"lat": 1.0, "lon": 1.0}),
		readsbRaw(t, "airplanes.live", "aaaaaa", t0.Add(time.Second), map[string]any{"lat": 1.0, "lon": 1.0}),
		readsbRaw(t, "airplanes.live", "bbbbbb", t0, map[string]any{"lat": 2.0, "lon": 2.0}),
	}

	got := MergeAll(raws)
	if len(got) != 2 {
		t.Fatalf("MergeAll returned %d states, want 2", len(got))
	}
	if got[0].ICAO24 != "aaaaaa" || got[1].ICAO24 != "bbbbbb" {
		t.Errorf("ICAO24s = [%s, %s], want [aaaaaa, bbbbbb]", got[0].ICAO24, got[1].ICAO24)
	}
	if want := []string{"adsb.lol", "airplanes.live"}; !equalStrings(got[0].Sources, want) {
		t.Errorf("aaaaaa Sources = %v, want %v", got[0].Sources, want)
	}
	if want := []string{"airplanes.live"}; !equalStrings(got[1].Sources, want) {
		t.Errorf("bbbbbb Sources = %v, want %v", got[1].Sources, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
