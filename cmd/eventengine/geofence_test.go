package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func squareZone(id, name string) flightmodel.Zone {
	return flightmodel.Zone{
		ID:   id,
		Name: name,
		Polygon: flightmodel.GeoJSONPolygon{
			Type: "Polygon",
			Coordinates: [][][]float64{
				{{-122.5, 37.5}, {-122.0, 37.5}, {-122.0, 38.0}, {-122.5, 38.0}, {-122.5, 37.5}},
			},
		},
	}
}

func TestGeofenceObserveNoEventOnFirstSighting(t *testing.T) {
	d := NewGeofenceDetector([]flightmodel.Zone{squareZone("z1", "SFO Approach")})
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	// Already inside the zone on first sighting: should not be reported as
	// an "enter" since there is no known prior state.
	events := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 37.75, Lon: -122.25, LastSeenUTC: now})
	if len(events) != 0 {
		t.Fatalf("Observe on first sighting = %d events, want 0", len(events))
	}
}

func TestGeofenceObserveTriggersEnter(t *testing.T) {
	d := NewGeofenceDetector([]flightmodel.Zone{squareZone("z1", "SFO Approach")})
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 39.0, Lon: -122.25, LastSeenUTC: now})
	events := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 37.75, Lon: -122.25, LastSeenUTC: now.Add(10 * time.Second)})

	if len(events) != 1 {
		t.Fatalf("Observe on crossing in = %d events, want 1", len(events))
	}
	got := events[0]
	if got.Type != flightmodel.EventTypeGeofenceEnter {
		t.Errorf("Type = %q, want %q", got.Type, flightmodel.EventTypeGeofenceEnter)
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

	var detail geofenceDetail
	if err := json.Unmarshal(got.Detail, &detail); err != nil {
		t.Fatalf("Unmarshal detail: %v", err)
	}
	if detail.ZoneID != "z1" {
		t.Errorf("ZoneID = %q, want %q", detail.ZoneID, "z1")
	}
	if detail.ZoneName != "SFO Approach" {
		t.Errorf("ZoneName = %q, want %q", detail.ZoneName, "SFO Approach")
	}
}

func TestGeofenceObserveTriggersExit(t *testing.T) {
	d := NewGeofenceDetector([]flightmodel.Zone{squareZone("z1", "SFO Approach")})
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 37.75, Lon: -122.25, LastSeenUTC: now})
	events := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 39.0, Lon: -122.25, LastSeenUTC: now.Add(10 * time.Second)})

	if len(events) != 1 {
		t.Fatalf("Observe on crossing out = %d events, want 1", len(events))
	}
	if events[0].Type != flightmodel.EventTypeGeofenceExit {
		t.Errorf("Type = %q, want %q", events[0].Type, flightmodel.EventTypeGeofenceExit)
	}
}

func TestGeofenceObserveNoEventWhenStayingInside(t *testing.T) {
	d := NewGeofenceDetector([]flightmodel.Zone{squareZone("z1", "SFO Approach")})
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 37.7, Lon: -122.3, LastSeenUTC: now})
	events := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 37.8, Lon: -122.2, LastSeenUTC: now.Add(10 * time.Second)})

	if len(events) != 0 {
		t.Fatalf("Observe while remaining inside = %d events, want 0", len(events))
	}
}

func TestGeofenceObserveNoEventWhenStayingOutside(t *testing.T) {
	d := NewGeofenceDetector([]flightmodel.Zone{squareZone("z1", "SFO Approach")})
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 39.0, Lon: -122.25, LastSeenUTC: now})
	events := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 40.0, Lon: -122.25, LastSeenUTC: now.Add(10 * time.Second)})

	if len(events) != 0 {
		t.Fatalf("Observe while remaining outside = %d events, want 0", len(events))
	}
}

func TestGeofenceObserveBoundaryCountsAsInside(t *testing.T) {
	d := NewGeofenceDetector([]flightmodel.Zone{squareZone("z1", "SFO Approach")})
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 39.0, Lon: -122.25, LastSeenUTC: now})
	events := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 37.5, Lon: -122.25, LastSeenUTC: now.Add(10 * time.Second)})

	if len(events) != 1 || events[0].Type != flightmodel.EventTypeGeofenceEnter {
		t.Fatalf("Observe onto boundary = %+v, want a single geofence_enter event", events)
	}
}

func TestGeofenceObserveEvaluatesMultipleZonesIndependently(t *testing.T) {
	zones := []flightmodel.Zone{squareZone("z1", "Zone One"), squareZone("z2", "Zone Two")}
	d := NewGeofenceDetector(zones)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	// Both zones share the same polygon here, so a single position update
	// inbound should trigger an enter for each zone independently.
	d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 39.0, Lon: -122.25, LastSeenUTC: now})
	events := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 37.75, Lon: -122.25, LastSeenUTC: now.Add(10 * time.Second)})

	if len(events) != 2 {
		t.Fatalf("Observe crossing into 2 zones = %d events, want 2", len(events))
	}
	seenZoneIDs := map[string]bool{}
	for _, e := range events {
		var detail geofenceDetail
		if err := json.Unmarshal(e.Detail, &detail); err != nil {
			t.Fatalf("Unmarshal detail: %v", err)
		}
		seenZoneIDs[detail.ZoneID] = true
	}
	if !seenZoneIDs["z1"] || !seenZoneIDs["z2"] {
		t.Errorf("seenZoneIDs = %v, want both z1 and z2", seenZoneIDs)
	}
}

func TestGeofenceObserveTracksMultipleAircraftIndependently(t *testing.T) {
	d := NewGeofenceDetector([]flightmodel.Zone{squareZone("z1", "SFO Approach")})
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	d.Observe(flightmodel.FlightState{ICAO24: "enter1", Lat: 39.0, Lon: -122.25, LastSeenUTC: now})
	d.Observe(flightmodel.FlightState{ICAO24: "steady1", Lat: 39.0, Lon: -122.25, LastSeenUTC: now})

	enterEvents := d.Observe(flightmodel.FlightState{ICAO24: "enter1", Lat: 37.75, Lon: -122.25, LastSeenUTC: now.Add(10 * time.Second)})
	steadyEvents := d.Observe(flightmodel.FlightState{ICAO24: "steady1", Lat: 39.5, Lon: -122.25, LastSeenUTC: now.Add(10 * time.Second)})

	if len(enterEvents) != 1 {
		t.Errorf("enter1 events = %d, want 1", len(enterEvents))
	}
	if len(steadyEvents) != 0 {
		t.Errorf("steady1 events = %d, want 0", len(steadyEvents))
	}
}

func TestGeofenceObserveNoZonesNeverTriggers(t *testing.T) {
	d := NewGeofenceDetector(nil)
	now := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)

	events := d.Observe(flightmodel.FlightState{ICAO24: "a1b2c3", Lat: 37.75, Lon: -122.25, LastSeenUTC: now})
	if len(events) != 0 {
		t.Fatalf("Observe with no zones = %d events, want 0", len(events))
	}
}
