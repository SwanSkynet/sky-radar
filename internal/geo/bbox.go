package geo

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// earthRadiusKM is used for the haversine distance calculation in
// BBox.RadiusKM.
const earthRadiusKM = 6371.0

// BBox is an axis-aligned latitude/longitude bounding box, used for
// GET /flights queries against the Redis geo index (see
// docs/architecture/data-model.md's flights:geo key).
type BBox struct {
	MinLon, MinLat, MaxLon, MaxLat float64
}

// ParseBBox parses the "minLon,minLat,maxLon,maxLat" query parameter
// format used by GET /flights, validating ranges and ordering.
func ParseBBox(s string) (BBox, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return BBox{}, fmt.Errorf("geo: bbox must have 4 comma-separated values (minLon,minLat,maxLon,maxLat), got %d", len(parts))
	}

	var vals [4]float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return BBox{}, fmt.Errorf("geo: bbox value %q is not a number", strings.TrimSpace(p))
		}
		vals[i] = v
	}

	b := BBox{MinLon: vals[0], MinLat: vals[1], MaxLon: vals[2], MaxLat: vals[3]}
	if err := b.Validate(); err != nil {
		return BBox{}, err
	}
	return b, nil
}

// Validate reports whether b is well-formed: coordinates in range, and
// min strictly less than max on each axis.
func (b BBox) Validate() error {
	if b.MinLon < -180 || b.MaxLon > 180 {
		return fmt.Errorf("geo: bbox longitude out of range [-180, 180]")
	}
	if b.MinLat < -90 || b.MaxLat > 90 {
		return fmt.Errorf("geo: bbox latitude out of range [-90, 90]")
	}
	if b.MinLon >= b.MaxLon {
		return fmt.Errorf("geo: bbox minLon must be less than maxLon")
	}
	if b.MinLat >= b.MaxLat {
		return fmt.Errorf("geo: bbox minLat must be less than maxLat")
	}
	return nil
}

// Contains reports whether (lat, lon) falls within b, inclusive of edges.
func (b BBox) Contains(lat, lon float64) bool {
	return lon >= b.MinLon && lon <= b.MaxLon && lat >= b.MinLat && lat <= b.MaxLat
}

// Center returns the midpoint of b.
func (b BBox) Center() (lon, lat float64) {
	return (b.MinLon + b.MaxLon) / 2, (b.MinLat + b.MaxLat) / 2
}

// RadiusKM returns the great-circle distance from b's center to its
// corner: the smallest radius that fully covers b. Callers use this to
// size a GEORADIUS query — Redis's deprecated-but-still-supported command;
// GEOSEARCH's BYBOX would be the modern equivalent, but isn't implemented
// by the miniredis test double this project relies on for unit tests — and
// then filter that circular (necessarily larger) result set down to the
// exact rectangle with Contains.
func (b BBox) RadiusKM() float64 {
	centerLon, centerLat := b.Center()
	return haversineKM(centerLat, centerLon, b.MaxLat, b.MaxLon)
}

// haversineKM returns the great-circle distance between two points in
// kilometers.
func haversineKM(lat1, lon1, lat2, lon2 float64) float64 {
	const rad = math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	lat1r := lat1 * rad
	lat2r := lat2 * rad

	sinDLat := math.Sin(dLat / 2)
	sinDLon := math.Sin(dLon / 2)
	a := sinDLat*sinDLat + math.Cos(lat1r)*math.Cos(lat2r)*sinDLon*sinDLon
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKM * c
}
