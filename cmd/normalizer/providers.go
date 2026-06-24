package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
)

// providerReport is one provider's raw payload mapped onto canonical
// FlightState fields, before merge precedence picks a winner across
// providers. See docs/api-docs/README.md's field mapping table.
type providerReport struct {
	Provider        string
	ICAO24          string
	LastSeenUTC     time.Time
	Callsign        *string
	Registration    *string
	Lat             float64
	Lon             float64
	AltitudeBaroFt  *int
	AltitudeGeoFt   *int
	GroundSpeedKt   *float64
	VerticalRateFpm *float64
	HeadingDeg      *float64
	OnGround        bool
	Squawk          *string
	PositionQuality flightmodel.PositionQuality
}

// ParseRawState maps a provider's raw payload onto a providerReport,
// dispatching on the provider name set by that provider's adapter.
func ParseRawState(raw sourceadapter.RawState) (providerReport, error) {
	switch raw.Provider {
	case "opensky":
		return parseOpenSkyStateVector(raw.Payload, raw.FetchedAt)
	case "adsb.lol", "airplanes.live":
		return parseReadsbAircraft(raw.Provider, raw.Payload, raw.FetchedAt)
	default:
		return providerReport{}, fmt.Errorf("normalizer: unknown provider %q", raw.Provider)
	}
}

// parseOpenSkyStateVector decodes one OpenSky /states/all positional array
// per the field order in docs/api-docs/opensky-api-docs.md.
func parseOpenSkyStateVector(payload json.RawMessage, fetchedAt time.Time) (providerReport, error) {
	var fields []json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky state vector: %w", err)
	}
	const minFields = 17
	if len(fields) < minFields {
		return providerReport{}, fmt.Errorf("normalizer: opensky state vector has %d fields, want at least %d", len(fields), minFields)
	}

	icao24, err := decodeString(fields[0])
	if err != nil || icao24 == "" {
		return providerReport{}, fmt.Errorf("normalizer: opensky state vector missing icao24")
	}
	callsign, err := decodeOptionalString(fields[1])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky callsign: %w", err)
	}
	callsign = trimToNil(callsign)
	lon, err := decodeOptionalFloat(fields[5])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky longitude: %w", err)
	}
	lat, err := decodeOptionalFloat(fields[6])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky latitude: %w", err)
	}
	baroAltM, err := decodeOptionalFloat(fields[7])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky baro_altitude: %w", err)
	}
	onGround, err := decodeBool(fields[8])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky on_ground: %w", err)
	}
	velocityMS, err := decodeOptionalFloat(fields[9])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky velocity: %w", err)
	}
	trueTrack, err := decodeOptionalFloat(fields[10])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky true_track: %w", err)
	}
	vertRateMS, err := decodeOptionalFloat(fields[11])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky vertical_rate: %w", err)
	}
	geoAltM, err := decodeOptionalFloat(fields[13])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky geo_altitude: %w", err)
	}
	squawk, err := decodeOptionalString(fields[14])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky squawk: %w", err)
	}
	positionSource, err := decodeOptionalInt(fields[16])
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode opensky position_source: %w", err)
	}

	return providerReport{
		Provider:        "opensky",
		ICAO24:          strings.ToLower(icao24),
		LastSeenUTC:     fetchedAt,
		Callsign:        callsign,
		Lat:             derefFloat(lat),
		Lon:             derefFloat(lon),
		AltitudeBaroFt:  metersToFeet(baroAltM),
		AltitudeGeoFt:   metersToFeet(geoAltM),
		GroundSpeedKt:   mpsToKnots(velocityMS),
		VerticalRateFpm: mpsToFeetPerMinute(vertRateMS),
		HeadingDeg:      trueTrack,
		OnGround:        onGround,
		Squawk:          squawk,
		PositionQuality: positionQualityFromOpenSkySource(positionSource),
	}, nil
}

// readsbAircraft mirrors the subset of the adsb.lol/airplanes.live
// "ac" array entry shape that the normalizer cares about; both providers
// share this readsb-derived schema. See docs/api-docs/airplanes-live-docs.md
// and docs/api-docs/adsb-lol-api-docs.md.
type readsbAircraft struct {
	Hex      string          `json:"hex"`
	Flight   *string         `json:"flight"`
	Reg      *string         `json:"r"`
	AltBaro  json.RawMessage `json:"alt_baro"`
	AltGeom  *float64        `json:"alt_geom"`
	GS       *float64        `json:"gs"`
	Track    *float64        `json:"track"`
	BaroRate *float64        `json:"baro_rate"`
	GeomRate *float64        `json:"geom_rate"`
	Squawk   *string         `json:"squawk"`
	Lat      *float64        `json:"lat"`
	Lon      *float64        `json:"lon"`
	Type     *string         `json:"type"`
}

// parseReadsbAircraft decodes one adsb.lol or airplanes.live aircraft
// object. provider distinguishes which adapter the payload came from (it
// is not present in the payload itself).
func parseReadsbAircraft(provider string, payload json.RawMessage, fetchedAt time.Time) (providerReport, error) {
	var ac readsbAircraft
	if err := json.Unmarshal(payload, &ac); err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode %s aircraft: %w", provider, err)
	}
	if ac.Hex == "" {
		return providerReport{}, fmt.Errorf("normalizer: %s aircraft missing hex", provider)
	}

	altBaroFt, onGround, err := parseAltBaro(ac.AltBaro)
	if err != nil {
		return providerReport{}, fmt.Errorf("normalizer: decode %s alt_baro: %w", provider, err)
	}

	var altGeomFt *int
	if ac.AltGeom != nil {
		v := int(math.Round(*ac.AltGeom))
		altGeomFt = &v
	}

	vertRate := ac.BaroRate
	if vertRate == nil {
		vertRate = ac.GeomRate
	}

	return providerReport{
		Provider:        provider,
		ICAO24:          strings.ToLower(ac.Hex),
		LastSeenUTC:     fetchedAt,
		Callsign:        trimToNil(ac.Flight),
		Registration:    trimToNil(ac.Reg),
		Lat:             derefFloat(ac.Lat),
		Lon:             derefFloat(ac.Lon),
		AltitudeBaroFt:  altBaroFt,
		AltitudeGeoFt:   altGeomFt,
		GroundSpeedKt:   ac.GS,
		VerticalRateFpm: vertRate,
		HeadingDeg:      ac.Track,
		OnGround:        onGround,
		Squawk:          ac.Squawk,
		PositionQuality: positionQualityFromType(ac.Type),
	}, nil
}

// parseAltBaro decodes readsb's polymorphic alt_baro field: either a
// numeric feet value, or the literal string "ground" (see
// docs/api-docs/airplanes-live-docs.md, point 9 in "AI Usage Notes").
func parseAltBaro(raw json.RawMessage) (altFt *int, onGround bool, err error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false, nil
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if s == "ground" {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("unexpected alt_baro string %q", s)
	}

	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, false, err
	}
	v := int(math.Round(f))
	return &v, false, nil
}

// positionQualityFromOpenSkySource maps OpenSky's position_source state
// vector field per docs/api-docs/opensky-api-docs.md (0=ADS-B, 2=MLAT;
// ASTERIX/FLARM and any unrecognized value are treated as estimated).
func positionQualityFromOpenSkySource(source *int) flightmodel.PositionQuality {
	if source == nil {
		return flightmodel.PositionQualityEstimated
	}
	switch *source {
	case 0:
		return flightmodel.PositionQualityADSB
	case 2:
		return flightmodel.PositionQualityMLAT
	default:
		return flightmodel.PositionQualityEstimated
	}
}

// positionQualityFromType maps readsb's "type" data-source field (see the
// table in docs/api-docs/airplanes-live-docs.md) onto PositionQuality. A
// missing type (adsb.lol does not always provide one) defaults to adsb,
// per docs/api-docs/README.md's field mapping table.
func positionQualityFromType(t *string) flightmodel.PositionQuality {
	if t == nil {
		return flightmodel.PositionQualityADSB
	}
	switch *t {
	case "mlat":
		return flightmodel.PositionQualityMLAT
	case "adsb_icao", "adsb_icao_nt", "adsb_other", "adsr_icao", "adsr_other":
		return flightmodel.PositionQualityADSB
	default:
		return flightmodel.PositionQualityEstimated
	}
}

func decodeString(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

func decodeOptionalString(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func decodeOptionalFloat(raw json.RawMessage) (*float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

func decodeOptionalInt(raw json.RawMessage) (*int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var i int
	if err := json.Unmarshal(raw, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

func decodeBool(raw json.RawMessage) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, err
	}
	return b, nil
}

func derefFloat(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func trimToNil(s *string) *string {
	if s == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func metersToFeet(m *float64) *int {
	if m == nil {
		return nil
	}
	v := int(math.Round(*m * 3.28084))
	return &v
}

func mpsToKnots(mps *float64) *float64 {
	if mps == nil {
		return nil
	}
	v := *mps * 1.94384
	return &v
}

func mpsToFeetPerMinute(mps *float64) *float64 {
	if mps == nil {
		return nil
	}
	v := *mps * 196.8504
	return &v
}
