package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

// StaleSignalDetector implements the stale-signal rule from
// docs/prd/phase-2-realtime-systems.md: an aircraft that stops appearing on
// flights.updates for longer than threshold has gone silent and should
// surface as an Event. Unlike the altitude/speed delta rules, staleness is
// detected by the *absence* of a message, so the detector keeps a small
// in-memory per-icao24 map (last-seen time + whether it has already been
// flagged for the current silence) and is swept on a timer rather than
// evaluated only on message receipt.
type StaleSignalDetector struct {
	threshold time.Duration

	mu       sync.Mutex
	lastSeen map[string]time.Time
	flagged  map[string]bool
}

// evictAfterMultiple bounds detector memory: once an aircraft has been
// flagged and remains silent for this many multiples of threshold, it is
// dropped from lastSeen/flagged entirely rather than tracked forever. If it
// reappears later, Observe just re-adds it.
const evictAfterMultiple = 6

// NewStaleSignalDetector returns a detector that flags an aircraft as
// stale once threshold has elapsed since its last observed flights.updates
// message.
func NewStaleSignalDetector(threshold time.Duration) *StaleSignalDetector {
	return &StaleSignalDetector{
		threshold: threshold,
		lastSeen:  make(map[string]time.Time),
		flagged:   make(map[string]bool),
	}
}

// Observe records that state was seen on flights.updates, and clears any
// existing stale flag for that aircraft: a fresh update means it is no
// longer silent, so a later gap can be detected again.
func (d *StaleSignalDetector) Observe(state flightmodel.FlightState) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastSeen[state.ICAO24] = state.LastSeenUTC
	delete(d.flagged, state.ICAO24)
}

// Sweep checks every tracked aircraft's last-seen time against now and
// returns one stale_signal Event for each aircraft that has just crossed
// threshold without a fresh Observe call. An aircraft already flagged for
// its current silence is not re-emitted on subsequent sweeps; Observe
// clears the flag so a future silence can trigger again.
func (d *StaleSignalDetector) Sweep(now time.Time) []flightmodel.Event {
	d.mu.Lock()
	defer d.mu.Unlock()

	var events []flightmodel.Event
	for icao24, lastSeen := range d.lastSeen {
		silentFor := now.Sub(lastSeen)
		if d.flagged[icao24] {
			if silentFor > d.threshold*evictAfterMultiple {
				delete(d.lastSeen, icao24)
				delete(d.flagged, icao24)
			}
			continue
		}
		if silentFor <= d.threshold {
			continue
		}
		d.flagged[icao24] = true
		events = append(events, newStaleSignalEvent(icao24, lastSeen, silentFor))
	}
	return events
}

// staleSignalDetail is the type-specific Event.Detail payload for
// stale_signal events, per docs/architecture/data-model.md.
type staleSignalDetail struct {
	LastSeenUTC     time.Time `json:"last_seen_utc"`
	StaleForSeconds int64     `json:"stale_for_seconds"`
}

// newStaleSignalEvent builds the Event for an aircraft that has gone
// silentFor longer than the detector's threshold. Severity is Warning
// rather than Notable/Info: an aircraft that has stopped reporting is a
// loss of track, which is a more actionable condition than a routine
// altitude/speed change.
func newStaleSignalEvent(icao24 string, lastSeen time.Time, silentFor time.Duration) flightmodel.Event {
	detail, _ := json.Marshal(staleSignalDetail{
		LastSeenUTC:     lastSeen,
		StaleForSeconds: int64(silentFor.Seconds()),
	})
	return flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeStaleSignal,
		ICAO24:        icao24,
		Severity:      flightmodel.EventSeverityWarning,
		OccurredAtUTC: lastSeen.Add(silentFor),
		Detail:        detail,
	}
}
