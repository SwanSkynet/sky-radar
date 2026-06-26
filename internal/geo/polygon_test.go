package geo

import (
	"testing"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func squareZonePolygon() flightmodel.GeoJSONPolygon {
	return flightmodel.GeoJSONPolygon{
		Type: "Polygon",
		Coordinates: [][][]float64{
			{{-122.5, 37.5}, {-122.0, 37.5}, {-122.0, 38.0}, {-122.5, 38.0}, {-122.5, 37.5}},
		},
	}
}

func TestPointInPolygonInsideSquare(t *testing.T) {
	if !PointInPolygon(squareZonePolygon(), 37.75, -122.25) {
		t.Fatal("PointInPolygon: want point inside square zone, got outside")
	}
}

func TestPointInPolygonOutsideSquare(t *testing.T) {
	if PointInPolygon(squareZonePolygon(), 39.0, -122.25) {
		t.Fatal("PointInPolygon: want point outside square zone, got inside")
	}
}

func TestPointInPolygonOnBoundary(t *testing.T) {
	if !PointInPolygon(squareZonePolygon(), 37.5, -122.25) {
		t.Fatal("PointInPolygon: want boundary point treated as inside, got outside")
	}
}

func TestPointInPolygonRespectsHole(t *testing.T) {
	donut := flightmodel.GeoJSONPolygon{
		Type: "Polygon",
		Coordinates: [][][]float64{
			{{-122.6, 37.4}, {-121.9, 37.4}, {-121.9, 38.1}, {-122.6, 38.1}, {-122.6, 37.4}},
			{{-122.5, 37.5}, {-122.0, 37.5}, {-122.0, 38.0}, {-122.5, 38.0}, {-122.5, 37.5}},
		},
	}

	if PointInPolygon(donut, 37.75, -122.25) {
		t.Fatal("PointInPolygon: want point inside the hole treated as outside, got inside")
	}
	if !PointInPolygon(donut, 37.45, -122.55) {
		t.Fatal("PointInPolygon: want point inside outer ring but outside hole treated as inside, got outside")
	}
}

func TestPointInPolygonEmptyPolygon(t *testing.T) {
	if PointInPolygon(flightmodel.GeoJSONPolygon{Type: "Polygon"}, 0, 0) {
		t.Fatal("PointInPolygon: want empty polygon to contain no points, got inside")
	}
}

func TestPointInPolygonSkipsMalformedCoordinatePairs(t *testing.T) {
	// A malformed entry ([-122.2] has only one element) must be skipped
	// rather than turned into a zero-value (0, 0) point, which would
	// otherwise distort the ring and corrupt containment checks for points
	// nowhere near the polygon's real vertices.
	polygon := flightmodel.GeoJSONPolygon{
		Type: "Polygon",
		Coordinates: [][][]float64{
			{{-122.5, 37.5}, {-122.0, 37.5}, {-122.2}, {-122.0, 38.0}, {-122.5, 38.0}, {-122.5, 37.5}},
		},
	}

	if !PointInPolygon(polygon, 37.75, -122.25) {
		t.Fatal("PointInPolygon: want point inside square zone despite malformed coordinate, got outside")
	}
	if PointInPolygon(polygon, 0, 0) {
		t.Fatal("PointInPolygon: want origin treated as outside, got inside (malformed coordinate likely produced a spurious (0,0) vertex)")
	}
}
