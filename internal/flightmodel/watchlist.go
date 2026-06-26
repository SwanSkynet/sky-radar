package flightmodel

import "time"

// WatchlistEntry is the canonical, durable representation of a user-tracked
// aircraft. See docs/architecture/data-model.md.
type WatchlistEntry struct {
	ID               string    `json:"id"`
	ICAO24           string    `json:"icao24"`
	Label            string    `json:"label"`
	CreatedBySession string    `json:"created_by_session"`
	CreatedAt        time.Time `json:"created_at"`
}
