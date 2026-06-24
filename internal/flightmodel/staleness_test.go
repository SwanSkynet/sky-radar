package flightmodel

import (
	"testing"
	"time"
)

func TestStaledMarksOldStateStale(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	f := FlightState{ICAO24: "a1b2c3", LastSeenUTC: now.Add(-90 * time.Second)}

	got := f.Staled(now)
	if !got.Stale {
		t.Error("Staled: Stale = false, want true for a report older than StaleThreshold")
	}
}

func TestStaledLeavesRecentStateFresh(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	f := FlightState{ICAO24: "a1b2c3", LastSeenUTC: now.Add(-10 * time.Second)}

	got := f.Staled(now)
	if got.Stale {
		t.Error("Staled: Stale = true, want false for a recent report")
	}
}

func TestStaledDoesNotMutateOtherFields(t *testing.T) {
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	f := FlightState{ICAO24: "a1b2c3", Lat: 1, Lon: 2, LastSeenUTC: now}

	got := f.Staled(now)
	if got.ICAO24 != f.ICAO24 || got.Lat != f.Lat || got.Lon != f.Lon {
		t.Errorf("Staled mutated unrelated fields: got %+v, want fields from %+v preserved", got, f)
	}
}
