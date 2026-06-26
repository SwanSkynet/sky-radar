package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
)

// watchlistAPI holds the dependencies for the session-scoped watchlist
// endpoints: POST/GET /watchlist and DELETE /watchlist/{id}. See
// docs/architecture/data-model.md's WatchlistEntry schema and
// docs/prd/phase-2-realtime-systems.md's watchlists/geofences requirement.
type watchlistAPI struct {
	pg     *pgstore.Store
	logger *slog.Logger
}

// createWatchlistEntryRequest is the POST /watchlist body: ID, CreatedAt,
// and CreatedBySession are server-assigned, not supplied by the client.
type createWatchlistEntryRequest struct {
	ICAO24 string `json:"icao24"`
	Label  string `json:"label"`
}

// createWatchlistEntry handles POST /watchlist.
func (a *watchlistAPI) createWatchlistEntry(w http.ResponseWriter, r *http.Request) {
	session, ok := requireSession(w, r)
	if !ok {
		return
	}

	var req createWatchlistEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.ICAO24 = strings.ToLower(strings.TrimSpace(req.ICAO24))
	req.Label = strings.TrimSpace(req.Label)
	if req.ICAO24 == "" {
		writeError(w, http.StatusBadRequest, "icao24 is required")
		return
	}
	if req.Label == "" {
		writeError(w, http.StatusBadRequest, "label is required")
		return
	}

	entry := flightmodel.WatchlistEntry{
		ID:               flightmodel.NewID(),
		ICAO24:           req.ICAO24,
		Label:            req.Label,
		CreatedBySession: session,
		CreatedAt:        time.Now().UTC(),
	}
	if err := a.pg.InsertWatchlistEntry(r.Context(), entry); err != nil {
		a.logger.Error("insert watchlist entry failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create watchlist entry")
		return
	}

	writeJSON(w, http.StatusCreated, entry)
}

// listWatchlistEntries handles GET /watchlist, returning only the
// requesting session's own entries.
func (a *watchlistAPI) listWatchlistEntries(w http.ResponseWriter, r *http.Request) {
	session, ok := requireSession(w, r)
	if !ok {
		return
	}

	entries, err := a.pg.ListWatchlistEntriesBySession(r.Context(), session)
	if err != nil {
		a.logger.Error("list watchlist entries failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list watchlist entries")
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// deleteWatchlistEntry handles DELETE /watchlist/{id}, scoped to the
// requesting session so one session cannot delete another's entry.
func (a *watchlistAPI) deleteWatchlistEntry(w http.ResponseWriter, r *http.Request) {
	session, ok := requireSession(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	deleted, err := a.pg.DeleteWatchlistEntry(r.Context(), id, session)
	if err != nil {
		a.logger.Error("delete watchlist entry failed", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "failed to delete watchlist entry")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "watchlist entry not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
