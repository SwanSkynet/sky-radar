package flightmodel

import "time"

// StaleThreshold is the staleness threshold backing the master PRD's
// degraded-mode SLO (docs/prd/00-master-prd.md): a FlightState not
// updated within this window is flagged stale rather than presented as
// current.
const StaleThreshold = 60 * time.Second

// Staled returns a copy of f with Stale recomputed relative to now, per
// the derived-field rule in docs/architecture/data-model.md. Staleness is
// relative to query time, not write time, so callers serving f should
// apply this at request time rather than trusting whatever Stale value
// was last written to Redis.
func (f FlightState) Staled(now time.Time) FlightState {
	f.Stale = now.Sub(f.LastSeenUTC) > StaleThreshold
	return f
}
