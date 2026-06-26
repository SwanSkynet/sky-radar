package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func TestWatchlistObserveTriggersMatchOnFirstSighting(t *testing.T) {
	d := NewWatchlistDetector([]flightmodel.WatchlistEntry{{ID: "w1", ICAO24: "a1b2c3", Label: "Friend's flight"}})
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	event, matched := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now})
	if !matched {
		t.Fatal("Observe on first sighting of watched aircraft = no match, want a match")
	}
	if event.Type != flightmodel.EventTypeWatchlistMatch {
		t.Errorf("Type = %q, want %q", event.Type, flightmodel.EventTypeWatchlistMatch)
	}
	if event.ICAO24 != "a1b2c3" {
		t.Errorf("ICAO24 = %q, want %q", event.ICAO24, "a1b2c3")
	}
	if event.Severity != flightmodel.EventSeverityNotable {
		t.Errorf("Severity = %q, want %q", event.Severity, flightmodel.EventSeverityNotable)
	}
	if event.ID == "" {
		t.Error("ID is empty, want a generated event id")
	}
	if !event.OccurredAtUTC.Equal(now) {
		t.Errorf("OccurredAtUTC = %v, want %v", event.OccurredAtUTC, now)
	}

	var detail watchlistDetail
	if err := json.Unmarshal(event.Detail, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.EntryID != "w1" {
		t.Errorf("EntryID = %q, want %q", detail.EntryID, "w1")
	}
	if detail.Label != "Friend's flight" {
		t.Errorf("Label = %q, want %q", detail.Label, "Friend's flight")
	}
}

func TestWatchlistObserveMatchesRegardlessOfICAO24Case(t *testing.T) {
	d := NewWatchlistDetector([]flightmodel.WatchlistEntry{{ID: "w1", ICAO24: "A1B2C3"}})
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	_, matched := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now})
	if !matched {
		t.Fatal("Observe with mixed-case watchlist entry vs lowercase FlightState ICAO24 = no match, want a match")
	}
}

func TestWatchlistObserveAircraftNotOnWatchlistNeverMatches(t *testing.T) {
	d := NewWatchlistDetector([]flightmodel.WatchlistEntry{{ID: "w1", ICAO24: "a1b2c3"}})
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	_, matched := d.Observe(flightmodel.FlightState{ICAO24: "z9z9z9", LastSeenUTC: now})
	if matched {
		t.Fatal("Observe for aircraft not on watchlist = match, want no match")
	}
}

func TestWatchlistObserveDoesNotRematchWhileStillInView(t *testing.T) {
	d := NewWatchlistDetector([]flightmodel.WatchlistEntry{{ID: "w1", ICAO24: "a1b2c3"}})
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now})
	_, matched := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now.Add(10 * time.Second)})
	if matched {
		t.Fatal("Observe on second consecutive sighting = match, want no match (already notified)")
	}
}

func TestWatchlistObserveNoEntriesNeverTriggers(t *testing.T) {
	d := NewWatchlistDetector(nil)
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	_, matched := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now})
	if matched {
		t.Fatal("Observe with no watchlist entries = match, want no match")
	}
}

func TestWatchlistObserveTracksMultipleAircraftIndependently(t *testing.T) {
	d := NewWatchlistDetector([]flightmodel.WatchlistEntry{
		{ID: "w1", ICAO24: "watched1"},
		{ID: "w2", ICAO24: "watched2"},
	})
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	_, matched1 := d.Observe(flightmodel.FlightState{ICAO24: "watched1", LastSeenUTC: now})
	_, matched2 := d.Observe(flightmodel.FlightState{ICAO24: "watched2", LastSeenUTC: now})
	if !matched1 || !matched2 {
		t.Fatalf("first sighting of two watched aircraft = (%v, %v), want (true, true)", matched1, matched2)
	}

	_, rematch1 := d.Observe(flightmodel.FlightState{ICAO24: "watched1", LastSeenUTC: now.Add(10 * time.Second)})
	if rematch1 {
		t.Error("watched1 second sighting = match, want no match")
	}
}

func TestWatchlistEvictBeforeAllowsRematchAfterEviction(t *testing.T) {
	d := NewWatchlistDetector([]flightmodel.WatchlistEntry{{ID: "w1", ICAO24: "a1b2c3"}})
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now})
	d.EvictBefore(now.Add(time.Second))

	_, matched := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now.Add(2 * time.Second)})
	if !matched {
		t.Fatal("Observe after eviction = no match, want a fresh match (state should be cleared)")
	}
}

func TestWatchlistEvictBeforeKeepsRecentAircraftUnmatched(t *testing.T) {
	d := NewWatchlistDetector([]flightmodel.WatchlistEntry{{ID: "w1", ICAO24: "a1b2c3"}})
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now})
	d.EvictBefore(now.Add(-time.Second))

	_, matched := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", LastSeenUTC: now.Add(10 * time.Second)})
	if matched {
		t.Fatal("Observe after no-op eviction = match, want no match (still in view)")
	}
}
