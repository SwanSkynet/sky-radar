package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

// WatchlistDetector implements the watchlist-match rule from
// docs/prd/phase-2-realtime-systems.md: an aircraft on a user's
// session-scoped watchlist becomes visible to the event engine. Unlike
// GeofenceDetector, there is no inside/outside transition to wait for —
// simply observing a watched ICAO24 at all is the match — so, unlike the
// geofence rule, a watchlist match fires on the very first sighting of a
// watched aircraft.
type WatchlistDetector struct {
	entries map[string]flightmodel.WatchlistEntry // icao24 -> entry

	mu             sync.Mutex
	matched        map[string]bool // icao24 -> already matched since last eviction
	lastObservedAt map[string]time.Time
}

// NewWatchlistDetector returns a detector that evaluates every observed
// FlightState against entries, keyed by ICAO24.
func NewWatchlistDetector(entries []flightmodel.WatchlistEntry) *WatchlistDetector {
	byICAO24 := make(map[string]flightmodel.WatchlistEntry, len(entries))
	for _, entry := range entries {
		byICAO24[entry.ICAO24] = entry
	}
	return &WatchlistDetector{
		entries:        byICAO24,
		matched:        make(map[string]bool),
		lastObservedAt: make(map[string]time.Time),
	}
}

// Observe returns a watchlist_match Event the first time state's ICAO24 is
// seen after being on the watchlist (or after its prior match was evicted
// by EvictBefore), and false otherwise — so a watched aircraft generates one
// notification per continuous period in view rather than one per update.
func (d *WatchlistDetector) Observe(state flightmodel.FlightState) (flightmodel.Event, bool) {
	entry, onWatchlist := d.entries[state.ICAO24]
	if !onWatchlist {
		return flightmodel.Event{}, false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.lastObservedAt[state.ICAO24] = state.LastSeenUTC
	if d.matched[state.ICAO24] {
		return flightmodel.Event{}, false
	}
	d.matched[state.ICAO24] = true

	return newWatchlistEvent(entry, state.LastSeenUTC), true
}

// EvictBefore removes tracked match state for aircraft last observed before
// cutoff, bounding matched's and lastObservedAt's growth for aircraft that
// have gone silent rather than retaining every ICAO24 ever seen for the
// life of the process. A watched aircraft that reappears after eviction is
// treated as a fresh sighting and matches again. Callers should pass a
// cutoff derived from the stale-signal threshold, mirroring
// GeofenceDetector.EvictBefore.
func (d *WatchlistDetector) EvictBefore(cutoff time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for icao24, lastObserved := range d.lastObservedAt {
		if lastObserved.Before(cutoff) {
			delete(d.matched, icao24)
			delete(d.lastObservedAt, icao24)
		}
	}
}

// watchlistDetail is the type-specific Event.Detail payload for
// watchlist_match events, per docs/architecture/data-model.md.
type watchlistDetail struct {
	EntryID string `json:"entry_id"`
	Label   string `json:"label"`
}

// newWatchlistEvent builds the Event for entry's tracked aircraft becoming
// visible at occurredAt. Severity is Notable, matching the geofence rule:
// a watchlist match is a deliberate, user-defined condition worth
// surfacing, but not inherently urgent the way a stale signal is.
func newWatchlistEvent(entry flightmodel.WatchlistEntry, occurredAt time.Time) flightmodel.Event {
	detail, _ := json.Marshal(watchlistDetail{
		EntryID: entry.ID,
		Label:   entry.Label,
	})
	return flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeWatchlistMatch,
		ICAO24:        entry.ICAO24,
		Severity:      flightmodel.EventSeverityNotable,
		OccurredAtUTC: occurredAt,
		Detail:        detail,
	}
}
