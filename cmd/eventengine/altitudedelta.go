package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

// AltitudeDeltaDetector implements the altitude-delta rule from
// docs/prd/phase-2-realtime-systems.md: a sudden change in an aircraft's
// barometric altitude between two consecutive flights.updates messages is
// surfaced as an Event. Unlike the stale-signal rule, this only ever needs
// the most recent prior reading, so it is evaluated synchronously on
// receipt of each update rather than on a sweep timer.
type AltitudeDeltaDetector struct {
	thresholdFt        int
	warningThresholdFt int

	mu             sync.Mutex
	lastAltitudeFt map[string]int
}

// NewAltitudeDeltaDetector returns a detector that flags an altitude_delta
// event when consecutive barometric-altitude readings for the same
// aircraft differ by at least thresholdFt, escalating to
// EventSeverityWarning once the difference reaches warningThresholdFt.
func NewAltitudeDeltaDetector(thresholdFt, warningThresholdFt int) *AltitudeDeltaDetector {
	return &AltitudeDeltaDetector{
		thresholdFt:        thresholdFt,
		warningThresholdFt: warningThresholdFt,
		lastAltitudeFt:     make(map[string]int),
	}
}

// Observe records state's barometric altitude as the new baseline for
// state.ICAO24 and reports whether the change since the previous baseline
// triggers an altitude_delta event. An aircraft with no prior recorded
// altitude (first sighting) never triggers, since there is nothing to
// compare against. An update with no barometric altitude (AltitudeBaroFt
// is nil) is ignored entirely: it neither triggers nor moves the baseline,
// so a later valid reading is still compared against the last known
// altitude rather than against nothing.
func (d *AltitudeDeltaDetector) Observe(state flightmodel.FlightState) (flightmodel.Event, bool) {
	if state.AltitudeBaroFt == nil {
		return flightmodel.Event{}, false
	}
	current := *state.AltitudeBaroFt

	d.mu.Lock()
	defer d.mu.Unlock()

	previous, tracked := d.lastAltitudeFt[state.ICAO24]
	d.lastAltitudeFt[state.ICAO24] = current
	if !tracked {
		return flightmodel.Event{}, false
	}

	delta := current - previous
	absDelta := delta
	if absDelta < 0 {
		absDelta = -absDelta
	}
	if absDelta < d.thresholdFt {
		return flightmodel.Event{}, false
	}

	severity := flightmodel.EventSeverityNotable
	if absDelta >= d.warningThresholdFt {
		severity = flightmodel.EventSeverityWarning
	}

	return newAltitudeDeltaEvent(state.ICAO24, state.LastSeenUTC, previous, current, delta, severity), true
}

// altitudeDeltaDetail is the type-specific Event.Detail payload for
// altitude_delta events, per docs/architecture/data-model.md.
type altitudeDeltaDetail struct {
	PreviousAltitudeFt int `json:"previous_altitude_ft"`
	CurrentAltitudeFt  int `json:"current_altitude_ft"`
	DeltaFt            int `json:"delta_ft"`
}

// newAltitudeDeltaEvent builds the Event for an aircraft whose barometric
// altitude moved from previous to current (delta = current - previous,
// signed) at occurredAt.
func newAltitudeDeltaEvent(icao24 string, occurredAt time.Time, previous, current, delta int, severity flightmodel.EventSeverity) flightmodel.Event {
	detail, _ := json.Marshal(altitudeDeltaDetail{
		PreviousAltitudeFt: previous,
		CurrentAltitudeFt:  current,
		DeltaFt:            delta,
	})
	return flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeAltitudeDelta,
		ICAO24:        icao24,
		Severity:      severity,
		OccurredAtUTC: occurredAt,
		Detail:        detail,
	}
}
