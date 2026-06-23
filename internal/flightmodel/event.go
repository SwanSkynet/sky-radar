package flightmodel

import (
	"encoding/json"
	"time"
)

// EventType enumerates the kinds of Event the event engine can emit.
type EventType string

const (
	EventTypeAltitudeDelta  EventType = "altitude_delta"
	EventTypeSpeedDelta     EventType = "speed_delta"
	EventTypeStaleSignal    EventType = "stale_signal"
	EventTypeGeofenceEnter  EventType = "geofence_enter"
	EventTypeGeofenceExit   EventType = "geofence_exit"
	EventTypeWatchlistMatch EventType = "watchlist_match"
)

// EventSeverity ranks an Event for display and alerting purposes.
type EventSeverity string

const (
	EventSeverityInfo    EventSeverity = "info"
	EventSeverityNotable EventSeverity = "notable"
	EventSeverityWarning EventSeverity = "warning"
)

// Event is the canonical, durable representation of a detected occurrence
// for a tracked aircraft. See docs/architecture/data-model.md.
type Event struct {
	ID            string          `json:"id"`
	Type          EventType       `json:"type"`
	ICAO24        string          `json:"icao24"`
	Severity      EventSeverity   `json:"severity"`
	OccurredAtUTC time.Time       `json:"occurred_at_utc"`
	Detail        json.RawMessage `json:"detail"`
}
