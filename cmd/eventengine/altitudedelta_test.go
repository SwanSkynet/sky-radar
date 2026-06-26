package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func intPtr(v int) *int { return &v }

func TestObserveNoEventOnFirstSighting(t *testing.T) {
	d := NewAltitudeDeltaDetector(1000, 3000)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	_, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(30000), LastSeenUTC: now})
	if ok {
		t.Fatal("Observe triggered on first sighting, want no event (no baseline yet)")
	}
}

func TestObserveTriggersAtThreshold(t *testing.T) {
	d := NewAltitudeDeltaDetector(1000, 3000)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(30000), LastSeenUTC: now})
	got, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(31000), LastSeenUTC: now.Add(10 * time.Second)})
	if !ok {
		t.Fatal("Observe did not trigger at threshold, want an event")
	}

	if got.Type != flightmodel.EventTypeAltitudeDelta {
		t.Errorf("Type = %q, want %q", got.Type, flightmodel.EventTypeAltitudeDelta)
	}
	if got.ICAO24 != "a1b2c3" {
		t.Errorf("ICAO24 = %q, want %q", got.ICAO24, "a1b2c3")
	}
	if got.Severity != flightmodel.EventSeverityNotable {
		t.Errorf("Severity = %q, want %q", got.Severity, flightmodel.EventSeverityNotable)
	}
	if got.ID == "" {
		t.Error("ID is empty, want a generated event id")
	}
	if !got.OccurredAtUTC.Equal(now.Add(10 * time.Second)) {
		t.Errorf("OccurredAtUTC = %v, want %v", got.OccurredAtUTC, now.Add(10*time.Second))
	}

	var detail altitudeDeltaDetail
	if err := json.Unmarshal(got.Detail, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.PreviousAltitudeFt != 30000 {
		t.Errorf("PreviousAltitudeFt = %d, want 30000", detail.PreviousAltitudeFt)
	}
	if detail.CurrentAltitudeFt != 31000 {
		t.Errorf("CurrentAltitudeFt = %d, want 31000", detail.CurrentAltitudeFt)
	}
	if detail.DeltaFt != 1000 {
		t.Errorf("DeltaFt = %d, want 1000", detail.DeltaFt)
	}
}

func TestObserveDoesNotTriggerBelowThreshold(t *testing.T) {
	d := NewAltitudeDeltaDetector(1000, 3000)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(30000), LastSeenUTC: now})
	_, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(30500), LastSeenUTC: now.Add(10 * time.Second)})
	if ok {
		t.Fatal("Observe triggered below threshold, want no event")
	}
}

func TestObserveTriggersOnDescent(t *testing.T) {
	d := NewAltitudeDeltaDetector(1000, 3000)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(30000), LastSeenUTC: now})
	got, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(28500), LastSeenUTC: now.Add(10 * time.Second)})
	if !ok {
		t.Fatal("Observe did not trigger on descent, want an event")
	}

	var detail altitudeDeltaDetail
	if err := json.Unmarshal(got.Detail, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.DeltaFt != -1500 {
		t.Errorf("DeltaFt = %d, want -1500 (signed)", detail.DeltaFt)
	}
}

func TestObserveEscalatesToWarningAtWarningThreshold(t *testing.T) {
	d := NewAltitudeDeltaDetector(1000, 3000)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(30000), LastSeenUTC: now})
	got, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(33000), LastSeenUTC: now.Add(10 * time.Second)})
	if !ok {
		t.Fatal("Observe did not trigger, want an event")
	}
	if got.Severity != flightmodel.EventSeverityWarning {
		t.Errorf("Severity = %q, want %q", got.Severity, flightmodel.EventSeverityWarning)
	}
}

func TestObserveIgnoresNilAltitudeReading(t *testing.T) {
	d := NewAltitudeDeltaDetector(1000, 3000)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(30000), LastSeenUTC: now})

	// A reading with no barometric altitude (e.g. on-ground/no Mode S data)
	// must neither trigger nor move the baseline.
	if _, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: nil, LastSeenUTC: now.Add(5 * time.Second)}); ok {
		t.Fatal("Observe triggered on a nil-altitude reading, want no event")
	}

	got, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(31500), LastSeenUTC: now.Add(10 * time.Second)})
	if !ok {
		t.Fatal("Observe did not trigger against last valid baseline, want an event")
	}

	var detail altitudeDeltaDetail
	if err := json.Unmarshal(got.Detail, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.PreviousAltitudeFt != 30000 {
		t.Errorf("PreviousAltitudeFt = %d, want 30000 (baseline unaffected by nil reading)", detail.PreviousAltitudeFt)
	}
}

func TestObserveResetsBaselineAfterTrigger(t *testing.T) {
	d := NewAltitudeDeltaDetector(1000, 3000)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(30000), LastSeenUTC: now})
	if _, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(31200), LastSeenUTC: now.Add(10 * time.Second)}); !ok {
		t.Fatal("1st delta did not trigger, want an event")
	}

	// Next reading is only 500ft from the new baseline (31200), below
	// threshold, even though it is 1700ft from the original baseline.
	if _, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", AltitudeBaroFt: intPtr(31700), LastSeenUTC: now.Add(20 * time.Second)}); ok {
		t.Fatal("2nd delta triggered, want no event (baseline should have reset after 1st trigger)")
	}
}

func TestObserveTracksMultipleAircraftIndependently(t *testing.T) {
	d := NewAltitudeDeltaDetector(1000, 3000)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "climb1", AltitudeBaroFt: intPtr(10000), LastSeenUTC: now})
	d.Observe(flightmodel.FlightState{ICAO24: "steady1", AltitudeBaroFt: intPtr(20000), LastSeenUTC: now})

	_, climbTriggered := d.Observe(flightmodel.FlightState{ICAO24: "climb1", AltitudeBaroFt: intPtr(11500), LastSeenUTC: now.Add(10 * time.Second)})
	_, steadyTriggered := d.Observe(flightmodel.FlightState{ICAO24: "steady1", AltitudeBaroFt: intPtr(20100), LastSeenUTC: now.Add(10 * time.Second)})

	if !climbTriggered {
		t.Error("climb1 did not trigger, want an event")
	}
	if steadyTriggered {
		t.Error("steady1 triggered, want no event")
	}
}
