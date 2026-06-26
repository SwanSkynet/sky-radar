package geo

import (
	"github.com/paulmach/orb"
	"github.com/paulmach/orb/planar"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

// PointInPolygon reports whether (lat, lon) falls within polygon, per the
// geofence point-in-polygon strategy in
// docs/architecture/system-architecture.md#geospatial-query-strategy: this
// runs in-process against GeoJSON rather than relying on PostGIS, since not
// every Postgres host has that extension available. The first ring is
// treated as the exterior boundary and any further rings as holes, per the
// GeoJSON Polygon spec (RFC 7946) that flightmodel.GeoJSONPolygon mirrors.
// Points exactly on a ring's edge are considered inside, matching
// BBox.Contains's inclusive-edge convention.
func PointInPolygon(polygon flightmodel.GeoJSONPolygon, lat, lon float64) bool {
	if len(polygon.Coordinates) == 0 {
		return false
	}

	rings := make(orb.Polygon, len(polygon.Coordinates))
	for i, ring := range polygon.Coordinates {
		points := make(orb.Ring, len(ring))
		for j, pos := range ring {
			if len(pos) < 2 {
				continue
			}
			points[j] = orb.Point{pos[0], pos[1]} // GeoJSON order: [lon, lat]
		}
		rings[i] = points
	}

	return planar.PolygonContains(rings, orb.Point{lon, lat})
}
