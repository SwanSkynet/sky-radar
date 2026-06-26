package main

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
)

var (
	errPolygonType     = errors.New(`polygon.type must be "Polygon"`)
	errPolygonRing     = errors.New("polygon must have at least one ring of at least 4 positions")
	errPolygonPosition = errors.New("polygon position must be [lon, lat]")
)

// zonesAPI holds the dependencies for the session-scoped geofence
// endpoints: POST/GET /zones and DELETE /zones/{id}. See
// docs/architecture/data-model.md's Zone schema and
// docs/prd/phase-2-realtime-systems.md's watchlists/geofences requirement.
type zonesAPI struct {
	pg     *pgstore.Store
	logger *slog.Logger
}

// createZoneRequest is the POST /zones body: every other Zone field is
// either server-generated (ID, CreatedAt) or read from sessionHeader
// (CreatedBySession), not supplied by the client.
type createZoneRequest struct {
	Name    string                    `json:"name"`
	Polygon flightmodel.GeoJSONPolygon `json:"polygon"`
}

// createZone handles POST /zones.
func (a *zonesAPI) createZone(w http.ResponseWriter, r *http.Request) {
	session, ok := requireSession(w, r)
	if !ok {
		return
	}

	var req createZoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := validatePolygon(req.Polygon); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	zone := flightmodel.Zone{
		ID:               flightmodel.NewID(),
		Name:             req.Name,
		Polygon:          req.Polygon,
		CreatedBySession: session,
		CreatedAt:        time.Now().UTC(),
	}
	if err := a.pg.InsertZone(r.Context(), zone); err != nil {
		a.logger.Error("insert zone failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create zone")
		return
	}

	writeJSON(w, http.StatusCreated, zone)
}

// listZones handles GET /zones, returning only the requesting session's
// own zones.
func (a *zonesAPI) listZones(w http.ResponseWriter, r *http.Request) {
	session, ok := requireSession(w, r)
	if !ok {
		return
	}

	zones, err := a.pg.ListZonesBySession(r.Context(), session)
	if err != nil {
		a.logger.Error("list zones failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list zones")
		return
	}
	writeJSON(w, http.StatusOK, zones)
}

// deleteZone handles DELETE /zones/{id}, scoped to the requesting session
// so one session cannot delete another's zone.
func (a *zonesAPI) deleteZone(w http.ResponseWriter, r *http.Request) {
	session, ok := requireSession(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	deleted, err := a.pg.DeleteZone(r.Context(), id, session)
	if err != nil {
		a.logger.Error("delete zone failed", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to delete zone")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "zone not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validatePolygon reports whether polygon is well-formed enough for
// geo.PointInPolygon to evaluate meaningfully: a GeoJSON Polygon with at
// least one ring of at least 4 positions (a closed triangle: 3 distinct
// vertices plus the closing repeat of the first).
func validatePolygon(polygon flightmodel.GeoJSONPolygon) error {
	if polygon.Type != "Polygon" {
		return errPolygonType
	}
	if len(polygon.Coordinates) == 0 || len(polygon.Coordinates[0]) < 4 {
		return errPolygonRing
	}
	for _, ring := range polygon.Coordinates {
		for _, pos := range ring {
			if len(pos) < 2 {
				return errPolygonPosition
			}
		}
	}
	return nil
}
