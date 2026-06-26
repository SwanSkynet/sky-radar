package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/geo"
)

// GeofenceDetector implements the geofence enter/exit rule from
// docs/prd/phase-2-realtime-systems.md: an aircraft crossing the boundary of
// a user-defined Zone is surfaced as a geofence_enter or geofence_exit
// Event. Point-in-polygon evaluation runs in-process against the zone set
// (see docs/architecture/system-architecture.md#geospatial-query-strategy)
// rather than as a live query, since it is a per-update rule evaluation
// like the other detectors in this package.
type GeofenceDetector struct {
	zones []flightmodel.Zone

	mu             sync.Mutex
	inside         map[string]map[string]bool // icao24 -> zone ID -> currently inside
	lastObservedAt map[string]time.Time
}

// NewGeofenceDetector returns a detector that evaluates every observed
// FlightState against zones.
func NewGeofenceDetector(zones []flightmodel.Zone) *GeofenceDetector {
	return &GeofenceDetector{
		zones:          zones,
		inside:         make(map[string]map[string]bool),
		lastObservedAt: make(map[string]time.Time),
	}
}

// Observe checks state's position against every zone and returns one
// geofence_enter or geofence_exit Event per zone whose containment state
// just changed since the previous observation of this aircraft. An
// aircraft's first observation against a given zone only establishes the
// baseline containment state and never triggers an event, mirroring
// AltitudeDeltaDetector's first-sighting behavior: there is no prior state
// to compare against, so treating "already inside on first sighting" as an
// enter would misreport an aircraft that was inside the zone before the
// engine ever started tracking it.
func (d *GeofenceDetector) Observe(state flightmodel.FlightState) []flightmodel.Event {
	d.mu.Lock()
	defer d.mu.Unlock()

	zoneStates, tracked := d.inside[state.ICAO24]
	if !tracked {
		zoneStates = make(map[string]bool)
		d.inside[state.ICAO24] = zoneStates
	}
	d.lastObservedAt[state.ICAO24] = state.LastSeenUTC

	var events []flightmodel.Event
	for _, zone := range d.zones {
		nowInside := geo.PointInPolygon(zone.Polygon, state.Lat, state.Lon)
		wasInside, hadBaseline := zoneStates[zone.ID]
		zoneStates[zone.ID] = nowInside

		if !hadBaseline || wasInside == nowInside {
			continue
		}

		if nowInside {
			events = append(events, newGeofenceEvent(flightmodel.EventTypeGeofenceEnter, state.ICAO24, zone, state.LastSeenUTC))
		} else {
			events = append(events, newGeofenceEvent(flightmodel.EventTypeGeofenceExit, state.ICAO24, zone, state.LastSeenUTC))
		}
	}
	return events
}

// EvictBefore removes tracked containment state for aircraft last observed
// before cutoff, bounding inside's growth for aircraft that have gone
// silent rather than retaining every ICAO24 ever seen for the life of the
// process. Callers should pass a cutoff derived from the stale-signal
// threshold, mirroring SpeedDeltaDetector.EvictBefore.
func (d *GeofenceDetector) EvictBefore(cutoff time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for icao24, lastObserved := range d.lastObservedAt {
		if lastObserved.Before(cutoff) {
			delete(d.inside, icao24)
			delete(d.lastObservedAt, icao24)
		}
	}
}

// geofenceDetail is the type-specific Event.Detail payload for
// geofence_enter/geofence_exit events, per docs/architecture/data-model.md.
type geofenceDetail struct {
	ZoneID   string `json:"zone_id"`
	ZoneName string `json:"zone_name"`
}

// newGeofenceEvent builds the Event for an aircraft crossing zone's
// boundary at occurredAt. Severity is Notable: a geofence crossing is a
// deliberate, user-defined condition worth surfacing, but not inherently
// urgent the way a stale signal is.
func newGeofenceEvent(eventType flightmodel.EventType, icao24 string, zone flightmodel.Zone, occurredAt time.Time) flightmodel.Event {
	detail, _ := json.Marshal(geofenceDetail{
		ZoneID:   zone.ID,
		ZoneName: zone.Name,
	})
	return flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          eventType,
		ICAO24:        icao24,
		Severity:      flightmodel.EventSeverityNotable,
		OccurredAtUTC: occurredAt,
		Detail:        detail,
	}
}
