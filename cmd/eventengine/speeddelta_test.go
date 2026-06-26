package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func floatPtr(v float64) *float64 { return &v }

func TestSpeedObserveNoEventOnFirstSighting(t *testing.T) {
	d := NewSpeedDeltaDetector(100, 250)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	_, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(450), LastSeenUTC: now})
	if ok {
		t.Fatal("Observe triggered on first sighting, want no event (no baseline yet)")
	}
}

func TestSpeedObserveTriggersAtThreshold(t *testing.T) {
	d := NewSpeedDeltaDetector(100, 250)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(450), LastSeenUTC: now})
	got, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(550), LastSeenUTC: now.Add(10 * time.Second)})
	if !ok {
		t.Fatal("Observe did not trigger at threshold, want an event")
	}

	if got.Type != flightmodel.EventTypeSpeedDelta {
		t.Errorf("Type = %q, want %q", got.Type, flightmodel.EventTypeSpeedDelta)
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

	var detail speedDeltaDetail
	if err := json.Unmarshal(got.Detail, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.PreviousSpeedKt != 450 {
		t.Errorf("PreviousSpeedKt = %v, want 450", detail.PreviousSpeedKt)
	}
	if detail.CurrentSpeedKt != 550 {
		t.Errorf("CurrentSpeedKt = %v, want 550", detail.CurrentSpeedKt)
	}
	if detail.DeltaKt != 100 {
		t.Errorf("DeltaKt = %v, want 100", detail.DeltaKt)
	}
}

func TestSpeedObserveDoesNotTriggerBelowThreshold(t *testing.T) {
	d := NewSpeedDeltaDetector(100, 250)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(450), LastSeenUTC: now})
	_, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(500), LastSeenUTC: now.Add(10 * time.Second)})
	if ok {
		t.Fatal("Observe triggered below threshold, want no event")
	}
}

func TestSpeedObserveTriggersOnDeceleration(t *testing.T) {
	d := NewSpeedDeltaDetector(100, 250)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(450), LastSeenUTC: now})
	got, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(300), LastSeenUTC: now.Add(10 * time.Second)})
	if !ok {
		t.Fatal("Observe did not trigger on deceleration, want an event")
	}

	var detail speedDeltaDetail
	if err := json.Unmarshal(got.Detail, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.DeltaKt != -150 {
		t.Errorf("DeltaKt = %v, want -150 (signed)", detail.DeltaKt)
	}
}

func TestSpeedObserveEscalatesToWarningAtWarningThreshold(t *testing.T) {
	d := NewSpeedDeltaDetector(100, 250)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(450), LastSeenUTC: now})
	got, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(700), LastSeenUTC: now.Add(10 * time.Second)})
	if !ok {
		t.Fatal("Observe did not trigger, want an event")
	}
	if got.Severity != flightmodel.EventSeverityWarning {
		t.Errorf("Severity = %q, want %q", got.Severity, flightmodel.EventSeverityWarning)
	}
}

func TestSpeedObserveIgnoresNilSpeedReading(t *testing.T) {
	d := NewSpeedDeltaDetector(100, 250)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(450), LastSeenUTC: now})

	// A reading with no ground speed (e.g. on-ground/no Mode S data) must
	// neither trigger nor move the baseline.
	if _, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: nil, LastSeenUTC: now.Add(5 * time.Second)}); ok {
		t.Fatal("Observe triggered on a nil-speed reading, want no event")
	}

	got, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(600), LastSeenUTC: now.Add(10 * time.Second)})
	if !ok {
		t.Fatal("Observe did not trigger against last valid baseline, want an event")
	}

	var detail speedDeltaDetail
	if err := json.Unmarshal(got.Detail, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.PreviousSpeedKt != 450 {
		t.Errorf("PreviousSpeedKt = %v, want 450 (baseline unaffected by nil reading)", detail.PreviousSpeedKt)
	}
}

func TestSpeedObserveResetsBaselineAfterTrigger(t *testing.T) {
	d := NewSpeedDeltaDetector(100, 250)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(450), LastSeenUTC: now})
	if _, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(560), LastSeenUTC: now.Add(10 * time.Second)}); !ok {
		t.Fatal("1st delta did not trigger, want an event")
	}

	// Next reading is only 40kt from the new baseline (560), below
	// threshold, even though it is 150kt from the original baseline.
	if _, ok := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", GroundSpeedKt: floatPtr(600), LastSeenUTC: now.Add(20 * time.Second)}); ok {
		t.Fatal("2nd delta triggered, want no event (baseline should have reset after 1st trigger)")
	}
}

func TestSpeedObserveTracksMultipleAircraftIndependently(t *testing.T) {
	d := NewSpeedDeltaDetector(100, 250)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "accel1", GroundSpeedKt: floatPtr(300), LastSeenUTC: now})
	d.Observe(flightmodel.FlightState{ICAO24: "steady1", GroundSpeedKt: floatPtr(400), LastSeenUTC: now})

	_, accelTriggered := d.Observe(flightmodel.FlightState{ICAO24: "accel1", GroundSpeedKt: floatPtr(420), LastSeenUTC: now.Add(10 * time.Second)})
	_, steadyTriggered := d.Observe(flightmodel.FlightState{ICAO24: "steady1", GroundSpeedKt: floatPtr(410), LastSeenUTC: now.Add(10 * time.Second)})

	if !accelTriggered {
		t.Error("accel1 did not trigger, want an event")
	}
	if steadyTriggered {
		t.Error("steady1 triggered, want no event")
	}
}
