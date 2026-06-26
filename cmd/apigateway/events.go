package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
)

// eventsAPI holds the dependencies for GET /events: the "event-type
// filters" part of the search/filter requirement in
// docs/prd/phase-2-realtime-systems.md.
type eventsAPI struct {
	pg     *pgstore.Store
	logger *slog.Logger
}

// listEvents handles GET /events?type=&icao24=&severity=&since=&limit=,
// every parameter optional.
func (a *eventsAPI) listEvents(w http.ResponseWriter, r *http.Request) {
	filter, err := parseEventFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	events, err := a.pg.QueryEvents(r.Context(), filter)
	if err != nil {
		a.logger.Error("query events failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to query events")
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func parseEventFilter(q map[string][]string) (pgstore.EventFilter, error) {
	get := func(key string) string {
		if vs, ok := q[key]; ok && len(vs) > 0 {
			return vs[0]
		}
		return ""
	}

	filter := pgstore.EventFilter{
		Type:     flightmodel.EventType(get("type")),
		ICAO24:   strings.ToLower(strings.TrimSpace(get("icao24"))),
		Severity: flightmodel.EventSeverity(get("severity")),
	}
	if filter.Type != "" && !isValidEventType(filter.Type) {
		return pgstore.EventFilter{}, fmt.Errorf("type must be one of: %s", validEventTypesList())
	}
	if filter.Severity != "" && !isValidEventSeverity(filter.Severity) {
		return pgstore.EventFilter{}, fmt.Errorf("severity must be one of: info, notable, warning")
	}

	if since := get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err != nil {
			return pgstore.EventFilter{}, fmt.Errorf("since must be an RFC3339 timestamp")
		}
		filter.Since = t.UTC()
	}

	if limit := get("limit"); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			return pgstore.EventFilter{}, fmt.Errorf("limit must be a positive integer")
		}
		filter.Limit = n
	}

	return filter, nil
}

func isValidEventType(t flightmodel.EventType) bool {
	switch t {
	case flightmodel.EventTypeAltitudeDelta, flightmodel.EventTypeSpeedDelta, flightmodel.EventTypeStaleSignal,
		flightmodel.EventTypeGeofenceEnter, flightmodel.EventTypeGeofenceExit, flightmodel.EventTypeWatchlistMatch:
		return true
	default:
		return false
	}
}

func isValidEventSeverity(s flightmodel.EventSeverity) bool {
	switch s {
	case flightmodel.EventSeverityInfo, flightmodel.EventSeverityNotable, flightmodel.EventSeverityWarning:
		return true
	default:
		return false
	}
}

func validEventTypesList() string {
	return "altitude_delta, speed_delta, stale_signal, geofence_enter, geofence_exit, watchlist_match"
}
