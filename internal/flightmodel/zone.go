package flightmodel

import "time"

// GeoJSONPolygon is a minimal GeoJSON Polygon geometry: Coordinates is a
// list of linear rings, each ring a list of [lon, lat] positions, per the
// GeoJSON spec (RFC 7946). The first ring is the exterior boundary; any
// further rings are holes.
type GeoJSONPolygon struct {
	Type        string        `json:"type"`
	Coordinates [][][]float64 `json:"coordinates"`
}

// Zone is the canonical, durable representation of a user-defined geofence.
// See docs/architecture/data-model.md.
type Zone struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Polygon          GeoJSONPolygon `json:"polygon"`
	CreatedBySession string         `json:"created_by_session"`
	CreatedAt        time.Time      `json:"created_at"`
}
