package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
)

// syntheticICAO24Block is the prefix used for every aircraft this harness
// generates. "fff" is in ICAO 24-bit address block range reserved/unused
// by any assigning state (real allocations are carved out of country
// blocks well below this range — see ICAO Annex 10 Vol III's address
// block listing), so synthetic load can't collide with a real icao24 if
// this harness is ever pointed at a staging environment that also has the
// real adapters running, and a tail subscriber can cheaply tell synthetic
// traffic apart from real traffic by prefix alone.
const syntheticICAO24Block = "fff"

// aircraft is one simulated flight the ingest-volume harness drives over
// the test duration. Position evolves by simple constant-heading dead
// reckoning each tick rather than teleporting to a fresh random point, so
// the generated traffic looks like real flight paths to the
// normalizer/event-engine rather than synthetic noise (per the harness's
// "realistic behavior" constraint).
type aircraft struct {
	icao24      string
	callsign    string
	lat, lon    float64
	altitudeFt  int
	groundSpdKt float64
	headingDeg  float64
	providers   []string
}

// newAircraftFleet builds n aircraft with randomized but plausible
// starting positions/kinematics, deterministically from seed so a run is
// reproducible. multiSourcePct (0..1) controls what fraction of aircraft
// are reported by two providers simultaneously (adsb.lol + airplanes.live
// both cover most of the same readsb-derived feeder network in reality),
// exercising the normalizer's multi-source merge path rather than only
// its single-source path.
func newAircraftFleet(n int, multiSourcePct float64, seed int64) []*aircraft {
	rng := rand.New(rand.NewSource(seed))
	fleet := make([]*aircraft, n)
	for i := 0; i < n; i++ {
		providers := []string{singleProviderFor(rng)}
		if rng.Float64() < multiSourcePct {
			providers = []string{"adsb.lol", "airplanes.live"}
		}

		fleet[i] = &aircraft{
			icao24:      fmt.Sprintf("%s%03x", syntheticICAO24Block, i),
			callsign:    fmt.Sprintf("LT%04d", i),
			lat:         rng.Float64()*160 - 80, // -80..80, away from pole singularities
			lon:         rng.Float64()*360 - 180,
			altitudeFt:  5000 + rng.Intn(35000),
			groundSpdKt: 250 + rng.Float64()*250,
			headingDeg:  rng.Float64() * 360,
			providers:   providers,
		}
	}
	return fleet
}

// singleProviderFor picks one of the three real provider names, weighted
// roughly toward the readsb-derived feeds since they're the higher-volume
// real-world sources per docs/api-docs/README.md's provider comparison.
func singleProviderFor(rng *rand.Rand) string {
	switch {
	case rng.Float64() < 0.5:
		return "adsb.lol"
	case rng.Float64() < 0.8:
		return "airplanes.live"
	default:
		return "opensky"
	}
}

// step advances the aircraft's position by elapsed at its current
// heading/speed (simple flat-earth dead reckoning — sufficient for
// generating plausible short-haul movement over a load-test run; not
// meant to be geodesically precise), wrapping at the antimeridian and
// clamping at the poles instead of crashing through them.
func (a *aircraft) step(elapsed time.Duration) {
	distanceNM := a.groundSpdKt * elapsed.Hours()
	distanceDeg := distanceNM / 60.0 // 1 degree of latitude ~= 60nm

	headingRad := a.headingDeg * math.Pi / 180
	a.lat += distanceDeg * math.Cos(headingRad)
	a.lon += distanceDeg * math.Sin(headingRad) / math.Cos(a.lat*math.Pi/180)

	if a.lat > 85 {
		a.lat = 85
		a.headingDeg = math.Mod(a.headingDeg+180, 360)
	}
	if a.lat < -85 {
		a.lat = -85
		a.headingDeg = math.Mod(a.headingDeg+180, 360)
	}
	a.lon = math.Mod(a.lon+540, 360) - 180 // wrap into [-180, 180)
}

// rawStates returns one sourceadapter.RawState per provider currently
// assigned to this aircraft, each carrying a payload shaped exactly like
// the real adsb.lol/airplanes.live/opensky adapters would have written
// (see cmd/normalizer/providers.go, the consumer this payload must satisfy)
// so the harness exercises the real parse path, not a shortcut format.
func (a *aircraft) rawStates(fetchedAt time.Time) []sourceadapter.RawState {
	states := make([]sourceadapter.RawState, 0, len(a.providers))
	for _, provider := range a.providers {
		var payload json.RawMessage
		switch provider {
		case "opensky":
			payload = a.openSkyPayload(fetchedAt)
		default:
			payload = a.readsbPayload()
		}
		states = append(states, sourceadapter.RawState{
			Provider:  provider,
			ICAO24:    a.icao24,
			FetchedAt: fetchedAt,
			Payload:   payload,
		})
	}
	return states
}

// readsbPayload builds the adsb.lol/airplanes.live "ac" array entry shape
// (see cmd/normalizer/providers.go's readsbAircraft and
// docs/api-docs/airplanes-live-docs.md).
func (a *aircraft) readsbPayload() json.RawMessage {
	data, err := json.Marshal(map[string]any{
		"hex":      a.icao24,
		"flight":   a.callsign,
		"alt_baro": a.altitudeFt,
		"gs":       a.groundSpdKt,
		"track":    a.headingDeg,
		"lat":      a.lat,
		"lon":      a.lon,
		"type":     "adsb_icao",
	})
	if err != nil {
		// Marshal of a map with only primitive values cannot fail.
		panic(err)
	}
	return data
}

// openSkyPayload builds the OpenSky /states/all positional-array shape
// (see cmd/normalizer/providers.go's parseOpenSkyStateVector and
// docs/api-docs/opensky-api-docs.md for the field order).
func (a *aircraft) openSkyPayload(fetchedAt time.Time) json.RawMessage {
	fields := []any{
		a.icao24,                        // 0 icao24
		a.callsign,                      // 1 callsign
		"US",                            // 2 origin_country
		float64(fetchedAt.Unix()),       // 3 time_position
		float64(fetchedAt.Unix()),       // 4 last_contact
		a.lon,                           // 5 longitude
		a.lat,                           // 6 latitude
		float64(a.altitudeFt) / 3.28084, // 7 baro_altitude (m)
		false,                           // 8 on_ground
		a.groundSpdKt / 1.94384,         // 9 velocity (m/s)
		a.headingDeg,                    // 10 true_track
		0.0,                             // 11 vertical_rate (m/s)
		nil,                             // 12 sensors
		float64(a.altitudeFt) / 3.28084, // 13 geo_altitude (m)
		"1200",                          // 14 squawk
		false,                           // 15 spi
		0,                               // 16 position_source (0 = ADS-B)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return data
}
