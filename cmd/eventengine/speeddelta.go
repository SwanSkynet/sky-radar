package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

// SpeedDeltaDetector implements the speed-delta rule from
// docs/prd/phase-2-realtime-systems.md: a sudden change in an aircraft's
// ground speed between two consecutive flights.updates messages is
// surfaced as an Event. Like the altitude-delta rule, this only ever needs
// the most recent prior reading, so it is evaluated synchronously on
// receipt of each update rather than on a sweep timer.
type SpeedDeltaDetector struct {
	thresholdKt        float64
	warningThresholdKt float64

	mu          sync.Mutex
	lastSpeedKt map[string]float64
}

// NewSpeedDeltaDetector returns a detector that flags a speed_delta event
// when consecutive ground-speed readings for the same aircraft differ by
// at least thresholdKt, escalating to EventSeverityWarning once the
// difference reaches warningThresholdKt.
func NewSpeedDeltaDetector(thresholdKt, warningThresholdKt float64) *SpeedDeltaDetector {
	return &SpeedDeltaDetector{
		thresholdKt:        thresholdKt,
		warningThresholdKt: warningThresholdKt,
		lastSpeedKt:        make(map[string]float64),
	}
}

// Observe records state's ground speed as the new baseline for
// state.ICAO24 and reports whether the change since the previous baseline
// triggers a speed_delta event. An aircraft with no prior recorded speed
// (first sighting) never triggers, since there is nothing to compare
// against. An update with no ground speed (GroundSpeedKt is nil) is
// ignored entirely: it neither triggers nor moves the baseline, so a later
// valid reading is still compared against the last known speed rather
// than against nothing.
func (d *SpeedDeltaDetector) Observe(state flightmodel.FlightState) (flightmodel.Event, bool) {
	if state.GroundSpeedKt == nil {
		return flightmodel.Event{}, false
	}
	current := *state.GroundSpeedKt

	d.mu.Lock()
	defer d.mu.Unlock()

	previous, tracked := d.lastSpeedKt[state.ICAO24]
	d.lastSpeedKt[state.ICAO24] = current
	if !tracked {
		return flightmodel.Event{}, false
	}

	delta := current - previous
	absDelta := delta
	if absDelta < 0 {
		absDelta = -absDelta
	}
	if absDelta < d.thresholdKt {
		return flightmodel.Event{}, false
	}

	severity := flightmodel.EventSeverityNotable
	if absDelta >= d.warningThresholdKt {
		severity = flightmodel.EventSeverityWarning
	}

	return newSpeedDeltaEvent(state.ICAO24, state.LastSeenUTC, previous, current, delta, severity), true
}

// speedDeltaDetail is the type-specific Event.Detail payload for
// speed_delta events, per docs/architecture/data-model.md.
type speedDeltaDetail struct {
	PreviousSpeedKt float64 `json:"previous_speed_kt"`
	CurrentSpeedKt  float64 `json:"current_speed_kt"`
	DeltaKt         float64 `json:"delta_kt"`
}

// newSpeedDeltaEvent builds the Event for an aircraft whose ground speed
// moved from previous to current (delta = current - previous, signed) at
// occurredAt.
func newSpeedDeltaEvent(icao24 string, occurredAt time.Time, previous, current, delta float64, severity flightmodel.EventSeverity) flightmodel.Event {
	detail, _ := json.Marshal(speedDeltaDetail{
		PreviousSpeedKt: previous,
		CurrentSpeedKt:  current,
		DeltaKt:         delta,
	})
	return flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeSpeedDelta,
		ICAO24:        icao24,
		Severity:      severity,
		OccurredAtUTC: occurredAt,
		Detail:        detail,
	}
}
