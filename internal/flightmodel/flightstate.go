package flightmodel

import "time"

// PositionQuality describes how a FlightState's position was derived,
// used to break near-tie merge conflicts between providers.
type PositionQuality string

const (
	PositionQualityADSB      PositionQuality = "adsb"
	PositionQualityMLAT      PositionQuality = "mlat"
	PositionQualityEstimated PositionQuality = "estimated"
)

// FlightState is the canonical, merged, in-memory and wire representation
// of a single tracked aircraft. See docs/architecture/data-model.md.
type FlightState struct {
	ICAO24          string          `json:"icao24"`
	Callsign        *string         `json:"callsign"`
	Registration    *string         `json:"registration"`
	Lat             float64         `json:"lat"`
	Lon             float64         `json:"lon"`
	AltitudeBaroFt  *int            `json:"altitude_baro_ft"`
	AltitudeGeoFt   *int            `json:"altitude_geo_ft"`
	GroundSpeedKt   *float64        `json:"ground_speed_kt"`
	VerticalRateFpm *float64        `json:"vertical_rate_fpm"`
	HeadingDeg      *float64        `json:"heading_deg"`
	OnGround        bool            `json:"on_ground"`
	Squawk          *string         `json:"squawk"`
	Sources         []string        `json:"sources"`
	PositionQuality PositionQuality `json:"position_quality"`
	LastSeenUTC     time.Time       `json:"last_seen_utc"`
	Stale           bool            `json:"stale"`
}
