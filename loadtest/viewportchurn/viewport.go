package main

import (
	"math/rand"

	"github.com/SwanSkynet/sky-radar/internal/geo"
)

// hotspot is a real-world high-traffic air-traffic region (major hub
// metro areas), used to seed each simulated client's starting viewport.
// docs/architecture/system-architecture.md is explicit that "a busy single
// region... is the right worst case to load-test against" since fan-out
// scales with viewers' viewports, not global aircraft count — so clients
// should cluster on a handful of busy regions rather than scatter
// uniformly across the globe, which would understate real-world per-
// region WS fan-out load.
type hotspot struct {
	name           string
	centerLon, lat float64
}

var hotspots = []hotspot{
	{"new-york-newark", -74.0, 40.7},
	{"london-heathrow", -0.45, 51.47},
	{"los-angeles", -118.4, 33.94},
	{"frankfurt", 8.57, 50.03},
	{"singapore", 103.99, 1.35},
	{"dubai", 55.36, 25.25},
	{"tokyo-haneda", 139.78, 35.55},
	{"atlanta", -84.43, 33.64},
}

// minHalfSpanDeg and maxHalfSpanDeg bound a simulated viewport's
// half-width/height in degrees: roughly a metro-area zoom level at the
// low end, a multi-state/country view at the high end, matching the
// range a real user pans/zooms a map client through rather than either a
// pixel-sized box or a whole-hemisphere box on every tick.
const (
	minHalfSpanDeg = 0.5
	maxHalfSpanDeg = 6.0

	// panStepDeg is the maximum per-tick drift of a viewport's center,
	// modeling a user panning the map a short distance rather than
	// teleporting across the globe between churn ticks.
	panStepDeg = 1.5

	// zoomStepFactor bounds how much a viewport's half-span can grow or
	// shrink in one churn tick (zoom in/out), again modeling a gradual
	// user gesture rather than an instant jump between extremes.
	zoomStepFactor = 0.3
)

// simulatedViewport is one client's evolving map viewport. center +
// halfSpan are tracked directly (rather than re-deriving them from the
// last BBox) so churn deltas compose cleanly tick over tick.
type simulatedViewport struct {
	centerLon, centerLat float64
	halfSpanDeg          float64
	rng                  *rand.Rand
}

// newSimulatedViewport starts a client at a randomly chosen hotspot with a
// randomly sized initial viewport, deterministic from seed.
func newSimulatedViewport(seed int64) *simulatedViewport {
	rng := rand.New(rand.NewSource(seed))
	h := hotspots[rng.Intn(len(hotspots))]
	return &simulatedViewport{
		centerLon:   h.centerLon,
		centerLat:   h.lat,
		halfSpanDeg: minHalfSpanDeg + rng.Float64()*(maxHalfSpanDeg-minHalfSpanDeg),
		rng:         rng,
	}
}

// churn applies one pan+zoom step, simulating a user panning/zooming the
// map, and returns the resulting BBox.
func (v *simulatedViewport) churn() geo.BBox {
	v.centerLon += (v.rng.Float64()*2 - 1) * panStepDeg
	v.centerLat += (v.rng.Float64()*2 - 1) * panStepDeg
	v.centerLat = clamp(v.centerLat, -85, 85)
	v.centerLon = wrapLon(v.centerLon)

	v.halfSpanDeg *= 1 + (v.rng.Float64()*2-1)*zoomStepFactor
	v.halfSpanDeg = clamp(v.halfSpanDeg, minHalfSpanDeg, maxHalfSpanDeg)

	return v.bbox()
}

// bbox returns the current viewport as a validated geo.BBox, clamping
// longitude span so a viewport centered near the antimeridian still
// produces a well-formed (minLon < maxLon) box instead of wrapping
// through it — real map clients handle the antimeridian as two boxes,
// which is unnecessary complexity for a load generator that just needs a
// valid, realistically sized box every tick.
func (v *simulatedViewport) bbox() geo.BBox {
	minLon := clamp(v.centerLon-v.halfSpanDeg, -180, 179)
	maxLon := clamp(v.centerLon+v.halfSpanDeg, minLon+0.01, 180)
	minLat := clamp(v.centerLat-v.halfSpanDeg, -90, 89)
	maxLat := clamp(v.centerLat+v.halfSpanDeg, minLat+0.01, 90)
	return geo.BBox{MinLon: minLon, MinLat: minLat, MaxLon: maxLon, MaxLat: maxLat}
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func wrapLon(lon float64) float64 {
	for lon < -180 {
		lon += 360
	}
	for lon > 180 {
		lon -= 360
	}
	return lon
}
