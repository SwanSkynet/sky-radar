package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/geo"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/redis/go-redis/v9"
)

// flightsAPI holds the dependencies for the minimal anonymous REST API:
// GET /flights (bbox query) and GET /flights/{icao24}. Both read
// exclusively from Redis hot state per docs/prd/phase-1-foundation.md's
// P1-FR4/P1-FR5 — no durable-history reads in this phase.
type flightsAPI struct {
	redis  *redisutil.Client
	logger *slog.Logger
}

// flightFilter narrows a GET /flights result set beyond the required bbox,
// per the "callsign/registration/ICAO24 search, altitude/speed band"
// requirement in docs/prd/phase-2-realtime-systems.md. Every field is
// optional; a zero-value field imposes no constraint. String fields match
// as a case-insensitive substring (not exact match) so a partial callsign
// or registration still finds the aircraft.
type flightFilter struct {
	callsign     string
	registration string
	icao24       string
	minAltFt     *int
	maxAltFt     *int
	minSpeedKt   *float64
	maxSpeedKt   *float64
}

// parseFlightFilter reads flightFilter's fields from q, returning an error
// describing exactly which parameter was malformed (used as the 400 body).
func parseFlightFilter(q map[string][]string) (flightFilter, error) {
	get := func(key string) string {
		if vs, ok := q[key]; ok && len(vs) > 0 {
			return vs[0]
		}
		return ""
	}

	f := flightFilter{
		callsign:     strings.ToLower(strings.TrimSpace(get("callsign"))),
		registration: strings.ToLower(strings.TrimSpace(get("registration"))),
		icao24:       strings.ToLower(strings.TrimSpace(get("icao24"))),
	}

	var err error
	if f.minAltFt, err = parseOptionalInt(get("min_altitude_ft")); err != nil {
		return flightFilter{}, fmt.Errorf("min_altitude_ft must be an integer")
	}
	if f.maxAltFt, err = parseOptionalInt(get("max_altitude_ft")); err != nil {
		return flightFilter{}, fmt.Errorf("max_altitude_ft must be an integer")
	}
	if f.minSpeedKt, err = parseOptionalFloat(get("min_speed_kt")); err != nil {
		return flightFilter{}, fmt.Errorf("min_speed_kt must be a number")
	}
	if f.maxSpeedKt, err = parseOptionalFloat(get("max_speed_kt")); err != nil {
		return flightFilter{}, fmt.Errorf("max_speed_kt must be a number")
	}
	return f, nil
}

// matches reports whether state satisfies every constraint f sets. A band
// constraint (e.g. minAltFt) excludes a state with no value for that field
// at all (e.g. AltitudeBaroFt == nil), since "is this aircraft between X
// and Y feet" has no truthful answer for an aircraft reporting no altitude.
func (f flightFilter) matches(state flightmodel.FlightState) bool {
	if f.callsign != "" && !containsFold(state.Callsign, f.callsign) {
		return false
	}
	if f.registration != "" && !containsFold(state.Registration, f.registration) {
		return false
	}
	if f.icao24 != "" && !strings.Contains(strings.ToLower(state.ICAO24), f.icao24) {
		return false
	}
	if f.minAltFt != nil && (state.AltitudeBaroFt == nil || *state.AltitudeBaroFt < *f.minAltFt) {
		return false
	}
	if f.maxAltFt != nil && (state.AltitudeBaroFt == nil || *state.AltitudeBaroFt > *f.maxAltFt) {
		return false
	}
	if f.minSpeedKt != nil && (state.GroundSpeedKt == nil || *state.GroundSpeedKt < *f.minSpeedKt) {
		return false
	}
	if f.maxSpeedKt != nil && (state.GroundSpeedKt == nil || *state.GroundSpeedKt > *f.maxSpeedKt) {
		return false
	}
	return true
}

func containsFold(field *string, substr string) bool {
	if field == nil {
		return false
	}
	return strings.Contains(strings.ToLower(*field), substr)
}

func parseOptionalInt(s string) (*int, error) {
	if s == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func parseOptionalFloat(s string) (*float64, error) {
	if s == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, err
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, fmt.Errorf("value must be finite")
	}
	return &v, nil
}

// listFlights handles GET /flights?bbox=minLon,minLat,maxLon,maxLat, plus
// the optional search/filter parameters parseFlightFilter reads (callsign,
// registration, icao24, min/max_altitude_ft, min/max_speed_kt).
func (a *flightsAPI) listFlights(w http.ResponseWriter, r *http.Request) {
	bboxParam := r.URL.Query().Get("bbox")
	if bboxParam == "" {
		writeError(w, http.StatusBadRequest, "bbox query parameter is required, e.g. bbox=minLon,minLat,maxLon,maxLat")
		return
	}

	bbox, err := geo.ParseBBox(bboxParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	filter, err := parseFlightFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	states, err := a.redis.QueryFlightsByBBox(r.Context(), bbox)
	if err != nil {
		a.logger.Error("query flights by bbox failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to query flights")
		return
	}

	now := time.Now().UTC()
	filtered := make([]flightmodel.FlightState, 0, len(states))
	for _, state := range states {
		state = state.Staled(now)
		if filter.matches(state) {
			filtered = append(filtered, state)
		}
	}

	writeJSON(w, http.StatusOK, filtered)
}

// getFlight handles GET /flights/{icao24}.
func (a *flightsAPI) getFlight(w http.ResponseWriter, r *http.Request) {
	icao24 := strings.ToLower(strings.TrimSpace(r.PathValue("icao24")))
	if icao24 == "" {
		writeError(w, http.StatusBadRequest, "icao24 is required")
		return
	}

	state, err := a.redis.ReadFlightState(r.Context(), icao24)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			writeError(w, http.StatusNotFound, "flight not currently tracked")
			return
		}
		a.logger.Error("read flight state failed", "icao24", icao24, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to read flight")
		return
	}

	writeJSON(w, http.StatusOK, state.Staled(time.Now().UTC()))
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
