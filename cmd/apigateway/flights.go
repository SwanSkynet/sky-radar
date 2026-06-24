package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

// listFlights handles GET /flights?bbox=minLon,minLat,maxLon,maxLat.
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

	states, err := a.redis.QueryFlightsByBBox(r.Context(), bbox)
	if err != nil {
		a.logger.Error("query flights by bbox failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to query flights")
		return
	}

	now := time.Now().UTC()
	for i := range states {
		states[i] = states[i].Staled(now)
	}

	writeJSON(w, http.StatusOK, states)
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
