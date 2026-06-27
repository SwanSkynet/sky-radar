package redisutil

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/geo"
	"github.com/redis/go-redis/v9"
)

// GeoSetKey is the Redis geo set backing bbox/radius queries over current
// flight positions. See docs/architecture/data-model.md's Redis key
// layout.
const GeoSetKey = "flights:geo"

// maxGeoRadiusKM bounds the radius QueryFlightsByBBox will hand to Redis
// GEORADIUS. Redis's geohash-based radius search silently returns an empty
// result once the search circle approaches planetary scale (a near-global
// bbox's center-to-corner distance is ~20000km), which manifested in
// production as a zoomed-out / whole-world viewport showing zero aircraft
// even though regional viewports returned data. Any viewport whose
// covering radius exceeds this threshold is answered by scanning the whole
// flights:geo set and filtering with BBox.Contains instead — always
// correct, and the geo set is bounded by the count of currently tracked
// aircraft. 5000km keeps city/regional/country viewports (the common case,
// radius well under ~3000km) on the indexed GEORADIUS path while routing
// continental-and-larger viewports to the scan.
const maxGeoRadiusKM = 5000

// FlightKey returns the Redis key for one aircraft's current canonical
// FlightState hash.
func FlightKey(icao24 string) string {
	return fmt.Sprintf("flight:%s", strings.ToLower(icao24))
}

// WriteFlightState writes state's canonical fields into its flight:{icao24}
// hash and indexes its position in the flights:geo geo set, both per
// docs/architecture/data-model.md. The hash carries ttl so a flight that
// stops being reported eventually disappears from Redis entirely — absence
// signals "no longer tracked," distinct from the Stale field which signals
// "tracked but not recently updated" (see docs/tech-stack/data-and-messaging.md).
//
// Every field is written on every call, even ones holding a nil/zero
// value in state, so that a winner that no longer reports a field (e.g.
// callsign) overwrites rather than leaves behind the previous winner's
// value in the hash.
func (c *Client) WriteFlightState(ctx context.Context, state flightmodel.FlightState, ttl time.Duration) error {
	icao24 := strings.ToLower(strings.TrimSpace(state.ICAO24))
	if icao24 == "" {
		return fmt.Errorf("redisutil: write flight state: icao24 is required")
	}

	key := FlightKey(icao24)

	pipe := c.rdb.TxPipeline()
	pipe.HSet(ctx, key, encodeFlightState(state))
	pipe.Expire(ctx, key, ttl)
	pipe.GeoAdd(ctx, GeoSetKey, &redis.GeoLocation{Name: icao24, Longitude: state.Lon, Latitude: state.Lat})
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("redisutil: write flight state %s: %w", key, err)
	}
	return nil
}

// ReadFlightState fetches and decodes the FlightState previously written
// by WriteFlightState, returning redis.Nil (via errors.Is) if it has
// expired or was never written.
func (c *Client) ReadFlightState(ctx context.Context, icao24 string) (flightmodel.FlightState, error) {
	key := FlightKey(icao24)

	fields, err := c.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("redisutil: read flight state %s: %w", key, err)
	}
	if len(fields) == 0 {
		return flightmodel.FlightState{}, fmt.Errorf("redisutil: read flight state %s: %w", key, redis.Nil)
	}

	state, err := decodeFlightState(fields)
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("redisutil: read flight state %s: %w", key, err)
	}
	return state, nil
}

// PruneExpiredGeoMembers removes flights:geo members whose flight:{icao24}
// hash has since expired. WriteFlightState's GEOADD has no way to expire
// itself when the hash TTLs out, so without an explicit prune pass the geo
// set would grow without bound as aircraft stop reporting (see
// docs/architecture/data-model.md's Redis key layout). Callers (the
// normalizer's merge loop) run this once per merge cycle.
func (c *Client) PruneExpiredGeoMembers(ctx context.Context) (int, error) {
	members, err := c.rdb.ZRange(ctx, GeoSetKey, 0, -1).Result()
	if err != nil {
		return 0, fmt.Errorf("redisutil: prune expired geo members: %w", err)
	}
	if len(members) == 0 {
		return 0, nil
	}

	pipe := c.rdb.Pipeline()
	exists := make([]*redis.IntCmd, len(members))
	for i, m := range members {
		exists[i] = pipe.Exists(ctx, FlightKey(m))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("redisutil: prune expired geo members: %w", err)
	}

	stale := make([]interface{}, 0, len(members))
	for i, m := range members {
		if exists[i].Val() == 0 {
			stale = append(stale, m)
		}
	}
	if len(stale) == 0 {
		return 0, nil
	}

	removed, err := c.rdb.ZRem(ctx, GeoSetKey, stale...).Result()
	if err != nil {
		return 0, fmt.Errorf("redisutil: prune expired geo members: %w", err)
	}
	return int(removed), nil
}

// QueryFlightsByBBox returns the current FlightState for every aircraft
// within bbox, querying the flights:geo geo index per
// docs/architecture/data-model.md. For viewports up to continental scale
// it uses Redis's GEORADIUS (rather than the newer GEOSEARCH BYBOX — see
// geo.BBox.RadiusKM for why) to size the initial candidate set; for larger
// viewports, where GEORADIUS's geohash search breaks down (see
// maxGeoRadiusKM), it falls back to enumerating the whole geo set. Either
// candidate set is then filtered down to bbox's exact rectangle with
// Contains before returning.
//
// A geo-set member whose hash has since expired (a race between the
// candidate lookup and the per-member read that follows it) is skipped
// rather than failing the whole query.
func (c *Client) QueryFlightsByBBox(ctx context.Context, bbox geo.BBox) ([]flightmodel.FlightState, error) {
	candidates, err := c.bboxCandidates(ctx, bbox)
	if err != nil {
		return nil, err
	}

	states := make([]flightmodel.FlightState, 0, len(candidates))
	for _, name := range candidates {
		state, err := c.ReadFlightState(ctx, name)
		if err != nil {
			// A member whose hash has since expired (the documented race
			// between the candidate lookup and this read) is skipped; any
			// other error (Redis failure, decode error) is real and surfaced
			// rather than silently yielding an incomplete flight list.
			if errors.Is(err, redis.Nil) {
				continue
			}
			return nil, err
		}
		if bbox.Contains(state.Lat, state.Lon) {
			states = append(states, state)
		}
	}

	sort.Slice(states, func(i, j int) bool { return states[i].ICAO24 < states[j].ICAO24 })
	return states, nil
}

// bboxCandidates returns the icao24 members to read and bbox-filter for a
// QueryFlightsByBBox call. It picks GEORADIUS for viewports Redis can
// reliably search and a full geo-set scan for ones too large for it (see
// maxGeoRadiusKM).
func (c *Client) bboxCandidates(ctx context.Context, bbox geo.BBox) ([]string, error) {
	if bbox.RadiusKM() > maxGeoRadiusKM {
		members, err := c.rdb.ZRange(ctx, GeoSetKey, 0, -1).Result()
		if err != nil {
			return nil, fmt.Errorf("redisutil: query flights by bbox (full scan): %w", err)
		}
		return members, nil
	}

	centerLon, centerLat := bbox.Center()

	//nolint:staticcheck // GeoRadius is deprecated in favor of GeoSearch, but
	// the miniredis test double this project relies on doesn't implement
	// GEOSEARCH; see the RadiusKM doc comment for the full rationale.
	locations, err := c.rdb.GeoRadius(ctx, GeoSetKey, centerLon, centerLat, &redis.GeoRadiusQuery{
		Radius: bbox.RadiusKM(),
		Unit:   "km",
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("redisutil: query flights by bbox: %w", err)
	}

	names := make([]string, 0, len(locations))
	for _, loc := range locations {
		names = append(names, loc.Name)
	}
	return names, nil
}

func encodeFlightState(state flightmodel.FlightState) map[string]any {
	return map[string]any{
		"icao24":            strings.ToLower(state.ICAO24),
		"callsign":          stringOrEmpty(state.Callsign),
		"registration":      stringOrEmpty(state.Registration),
		"lat":               strconv.FormatFloat(state.Lat, 'f', -1, 64),
		"lon":               strconv.FormatFloat(state.Lon, 'f', -1, 64),
		"altitude_baro_ft":  intOrEmpty(state.AltitudeBaroFt),
		"altitude_geo_ft":   intOrEmpty(state.AltitudeGeoFt),
		"ground_speed_kt":   floatOrEmpty(state.GroundSpeedKt),
		"vertical_rate_fpm": floatOrEmpty(state.VerticalRateFpm),
		"heading_deg":       floatOrEmpty(state.HeadingDeg),
		"on_ground":         strconv.FormatBool(state.OnGround),
		"squawk":            stringOrEmpty(state.Squawk),
		"sources":           strings.Join(state.Sources, ","),
		"position_quality":  string(state.PositionQuality),
		"last_seen_utc":     state.LastSeenUTC.UTC().Format(time.RFC3339Nano),
		"aircraft_type":     stringOrEmpty(state.AircraftType),
		"emitter_category":  stringOrEmpty(state.EmitterCategory),
		"military":          strconv.FormatBool(state.Military),
		"icon_class":        stringOrEmpty(state.IconClass),
	}
}

func decodeFlightState(fields map[string]string) (flightmodel.FlightState, error) {
	lat, err := strconv.ParseFloat(fields["lat"], 64)
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("decode lat: %w", err)
	}
	lon, err := strconv.ParseFloat(fields["lon"], 64)
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("decode lon: %w", err)
	}
	onGround, err := strconv.ParseBool(fields["on_ground"])
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("decode on_ground: %w", err)
	}
	lastSeen, err := time.Parse(time.RFC3339Nano, fields["last_seen_utc"])
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("decode last_seen_utc: %w", err)
	}

	altitudeBaroFt, err := intOrNil(fields["altitude_baro_ft"])
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("decode altitude_baro_ft: %w", err)
	}
	altitudeGeoFt, err := intOrNil(fields["altitude_geo_ft"])
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("decode altitude_geo_ft: %w", err)
	}
	groundSpeedKt, err := floatOrNil(fields["ground_speed_kt"])
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("decode ground_speed_kt: %w", err)
	}
	verticalRateFpm, err := floatOrNil(fields["vertical_rate_fpm"])
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("decode vertical_rate_fpm: %w", err)
	}
	headingDeg, err := floatOrNil(fields["heading_deg"])
	if err != nil {
		return flightmodel.FlightState{}, fmt.Errorf("decode heading_deg: %w", err)
	}

	var sources []string
	if v := fields["sources"]; v != "" {
		sources = strings.Split(v, ",")
	}

	// military may be absent on hashes written before this field existed;
	// treat absent/empty as false rather than a decode error.
	var military bool
	if v := fields["military"]; v != "" {
		military, err = strconv.ParseBool(v)
		if err != nil {
			return flightmodel.FlightState{}, fmt.Errorf("decode military: %w", err)
		}
	}

	return flightmodel.FlightState{
		ICAO24:          fields["icao24"],
		Callsign:        stringOrNil(fields["callsign"]),
		Registration:    stringOrNil(fields["registration"]),
		Lat:             lat,
		Lon:             lon,
		AltitudeBaroFt:  altitudeBaroFt,
		AltitudeGeoFt:   altitudeGeoFt,
		GroundSpeedKt:   groundSpeedKt,
		VerticalRateFpm: verticalRateFpm,
		HeadingDeg:      headingDeg,
		OnGround:        onGround,
		Squawk:          stringOrNil(fields["squawk"]),
		Sources:         sources,
		PositionQuality: flightmodel.PositionQuality(fields["position_quality"]),
		LastSeenUTC:     lastSeen,
		AircraftType:    stringOrNil(fields["aircraft_type"]),
		EmitterCategory: stringOrNil(fields["emitter_category"]),
		Military:        military,
		IconClass:       stringOrNil(fields["icon_class"]),
	}, nil
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intOrEmpty(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func floatOrEmpty(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func stringOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intOrNil(s string) (*int, error) {
	if s == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func floatOrNil(s string) (*float64, error) {
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
