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

	// AircraftType is the raw ICAO type designator (e.g. "A320"), captured
	// from adsb.lol / airplanes.live. Nil for OpenSky-sourced aircraft,
	// whose feed carries no type. See docs/architecture/data-model.md.
	AircraftType *string `json:"aircraft_type"`
	// EmitterCategory is the ADS-B emitter category (e.g. "A5", "A7"),
	// captured from adsb.lol / airplanes.live; nil when unavailable.
	EmitterCategory *string `json:"emitter_category"`
	// Military reports whether a provider flagged this aircraft as military
	// (adsb.lol / airplanes.live dbFlags bit 1). False when unknown.
	Military bool `json:"military"`
	// IconClass is the derived icon bucket (one of the SVG names in
	// web/src/assets, e.g. "commercial_jet", "awacs"), computed by the
	// classifier from AircraftType / EmitterCategory / Military. Nil when
	// nothing classifiable was available (the frontend then draws a default).
	IconClass *string `json:"icon_class"`
}
