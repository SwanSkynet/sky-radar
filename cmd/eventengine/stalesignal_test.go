package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func TestSweepFlagsAircraftPastThreshold(t *testing.T) {
	d := NewStaleSignalDetector(60 * time.Second)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now.Add(-90 * time.Second)})

	events := d.Sweep(now)
	if len(events) != 1 {
		t.Fatalf("Sweep returned %d events, want 1", len(events))
	}

	got := events[0]
	if got.Type != flightmodel.EventTypeStaleSignal {
		t.Errorf("Type = %q, want %q", got.Type, flightmodel.EventTypeStaleSignal)
	}
	if got.ICAO24 != "a1b2c3" {
		t.Errorf("ICAO24 = %q, want %q", got.ICAO24, "a1b2c3")
	}
	if got.Severity != flightmodel.EventSeverityWarning {
		t.Errorf("Severity = %q, want %q", got.Severity, flightmodel.EventSeverityWarning)
	}
	if got.ID == "" {
		t.Error("ID is empty, want a generated event id")
	}
	if !got.OccurredAtUTC.Equal(now) {
		t.Errorf("OccurredAtUTC = %v, want %v", got.OccurredAtUTC, now)
	}

	var detail staleSignalDetail
	if err := json.Unmarshal(got.Detail, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.StaleForSeconds != 90 {
		t.Errorf("StaleForSeconds = %d, want 90", detail.StaleForSeconds)
	}
	if !detail.LastSeenUTC.Equal(now.Add(-90 * time.Second)) {
		t.Errorf("LastSeenUTC = %v, want %v", detail.LastSeenUTC, now.Add(-90*time.Second))
	}
}

func TestSweepDoesNotFlagAircraftWithinThreshold(t *testing.T) {
	d := NewStaleSignalDetector(60 * time.Second)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now.Add(-10 * time.Second)})

	events := d.Sweep(now)
	if len(events) != 0 {
		t.Fatalf("Sweep returned %d events, want 0 for a recently observed aircraft", len(events))
	}
}

func TestSweepDoesNotReemitForAlreadyFlaggedAircraft(t *testing.T) {
	d := NewStaleSignalDetector(60 * time.Second)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now.Add(-90 * time.Second)})

	if events := d.Sweep(now); len(events) != 1 {
		t.Fatalf("1st Sweep returned %d events, want 1", len(events))
	}
	if events := d.Sweep(now.Add(30 * time.Second)); len(events) != 0 {
		t.Fatalf("2nd Sweep returned %d events, want 0 (already flagged)", len(events))
	}
}

func TestObserveClearsFlagSoLaterSilenceRetriggers(t *testing.T) {
	d := NewStaleSignalDetector(60 * time.Second)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now.Add(-90 * time.Second)})
	if events := d.Sweep(now); len(events) != 1 {
		t.Fatalf("1st Sweep returned %d events, want 1", len(events))
	}

	// Aircraft reappears, then goes silent again later.
	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now})

	later := now.Add(90 * time.Second)
	events := d.Sweep(later)
	if len(events) != 1 {
		t.Fatalf("Sweep after re-observe returned %d events, want 1", len(events))
	}
}

func TestSweepIgnoresUntrackedAircraft(t *testing.T) {
	d := NewStaleSignalDetector(60 * time.Second)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	if events := d.Sweep(now); len(events) != 0 {
		t.Fatalf("Sweep on empty detector returned %d events, want 0", len(events))
	}
}

func TestSweepHandlesMultipleAircraftIndependently(t *testing.T) {
	d := NewStaleSignalDetector(60 * time.Second)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "stale1", LastSeenUTC: now.Add(-90 * time.Second)})
	d.Observe(flightmodel.FlightState{ICAO24: "fresh1", LastSeenUTC: now.Add(-5 * time.Second)})
	d.Observe(flightmodel.FlightState{ICAO24: "stale2", LastSeenUTC: now.Add(-120 * time.Second)})

	events := d.Sweep(now)
	if len(events) != 2 {
		t.Fatalf("Sweep returned %d events, want 2", len(events))
	}

	got := map[string]bool{}
	for _, e := range events {
		got[e.ICAO24] = true
	}
	if !got["stale1"] || !got["stale2"] {
		t.Errorf("got events for %v, want stale1 and stale2", got)
	}
	if got["fresh1"] {
		t.Error("got event for fresh1, want none")
	}
}
