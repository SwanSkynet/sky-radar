package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func testWatchlistRouter(t *testing.T) http.Handler {
	t.Helper()
	pg := testReplayPostgres(t)
	api, _ := testAPI(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newRouterWithExtras(api, nil, nil, nil, &watchlistAPI{pg: pg, logger: logger}, nil)
}

func TestCreateWatchlistEntryRequiresSessionHeader(t *testing.T) {
	mux := testWatchlistRouter(t)

	body, _ := json.Marshal(map[string]string{"icao24": "a1b2c3", "label": "Friend's flight"})
	req := httptest.NewRequest(http.MethodPost, "/watchlist", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreateWatchlistEntryRequiresICAO24(t *testing.T) {
	mux := testWatchlistRouter(t)

	body, _ := json.Marshal(map[string]string{"label": "Missing icao24"})
	req := httptest.NewRequest(http.MethodPost, "/watchlist", bytes.NewReader(body))
	req.Header.Set(sessionHeader, "session-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreateAndListWatchlistEntryRoundTrip(t *testing.T) {
	mux := testWatchlistRouter(t)
	session := fmt.Sprintf("session-%s", t.Name())

	body, _ := json.Marshal(map[string]string{"icao24": "A1B2C3", "label": "Friend's flight"})
	req := httptest.NewRequest(http.MethodPost, "/watchlist", bytes.NewReader(body))
	req.Header.Set(sessionHeader, session)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created flightmodel.WatchlistEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal create response: %v", err)
	}
	if created.ID == "" || created.ICAO24 != "a1b2c3" || created.Label != "Friend's flight" {
		t.Fatalf("created entry = %+v, unexpected (icao24 should be lowercased)", created)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/watchlist", nil)
	listReq.Header.Set(sessionHeader, session)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body = %s", listRec.Code, http.StatusOK, listRec.Body.String())
	}

	var entries []flightmodel.WatchlistEntry
	if err := json.Unmarshal(listRec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("Unmarshal list response: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("created entry %s not present in list response: %+v", created.ID, entries)
	}
}

func TestDeleteWatchlistEntryRejectsWrongSession(t *testing.T) {
	mux := testWatchlistRouter(t)
	session := fmt.Sprintf("session-%s", t.Name())

	body, _ := json.Marshal(map[string]string{"icao24": "a1b2c3", "label": "Owned"})
	createReq := httptest.NewRequest(http.MethodPost, "/watchlist", bytes.NewReader(body))
	createReq.Header.Set(sessionHeader, session)
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	var created flightmodel.WatchlistEntry
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/watchlist/"+created.ID, nil)
	delReq.Header.Set(sessionHeader, "a-different-session")
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (wrong session should not see the entry)", delRec.Code, http.StatusNotFound)
	}

	delOwnReq := httptest.NewRequest(http.MethodDelete, "/watchlist/"+created.ID, nil)
	delOwnReq.Header.Set(sessionHeader, session)
	delOwnRec := httptest.NewRecorder()
	mux.ServeHTTP(delOwnRec, delOwnReq)
	if delOwnRec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", delOwnRec.Code, http.StatusNoContent)
	}
}
