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

func testZonesRouter(t *testing.T) http.Handler {
	t.Helper()
	pg := testReplayPostgres(t)
	api, _ := testAPI(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newRouterWithExtras(api, nil, nil, &zonesAPI{pg: pg, logger: logger}, nil, nil, nil)
}

func validPolygonBody(name string) []byte {
	body, _ := json.Marshal(map[string]any{
		"name": name,
		"polygon": flightmodel.GeoJSONPolygon{
			Type: "Polygon",
			Coordinates: [][][]float64{{
				{-122.5, 37.5}, {-122.0, 37.5}, {-122.0, 38.0}, {-122.5, 37.5},
			}},
		},
	})
	return body
}

func TestCreateZoneRequiresSessionHeader(t *testing.T) {
	mux := testZonesRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/zones", bytes.NewReader(validPolygonBody("No Session")))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreateZoneRejectsInvalidPolygon(t *testing.T) {
	mux := testZonesRouter(t)

	body, _ := json.Marshal(map[string]any{"name": "Bad Zone", "polygon": map[string]any{"type": "Point"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/zones", bytes.NewReader(body))
	req.Header.Set(sessionHeader, "session-1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreateAndListZoneRoundTrip(t *testing.T) {
	mux := testZonesRouter(t)
	session := fmt.Sprintf("session-%s", t.Name())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/zones", bytes.NewReader(validPolygonBody("My Zone")))
	req.Header.Set(sessionHeader, session)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var created flightmodel.Zone
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal create response: %v", err)
	}
	if created.ID == "" || created.Name != "My Zone" || created.CreatedBySession != session {
		t.Fatalf("created zone = %+v, unexpected", created)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/zones", nil)
	listReq.Header.Set(sessionHeader, session)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d, body = %s", listRec.Code, http.StatusOK, listRec.Body.String())
	}

	var zones []flightmodel.Zone
	if err := json.Unmarshal(listRec.Body.Bytes(), &zones); err != nil {
		t.Fatalf("Unmarshal list response: %v", err)
	}
	found := false
	for _, z := range zones {
		if z.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("created zone %s not present in list response: %+v", created.ID, zones)
	}
}

func TestDeleteZoneRemovesIt(t *testing.T) {
	mux := testZonesRouter(t)
	session := fmt.Sprintf("session-%s", t.Name())

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/zones", bytes.NewReader(validPolygonBody("To Delete")))
	createReq.Header.Set(sessionHeader, session)
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	var created flightmodel.Zone
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/zones/"+created.ID, nil)
	delReq.Header.Set(sessionHeader, session)
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d, body = %s", delRec.Code, http.StatusNoContent, delRec.Body.String())
	}

	delAgainReq := httptest.NewRequest(http.MethodDelete, "/api/v1/zones/"+created.ID, nil)
	delAgainReq.Header.Set(sessionHeader, session)
	delAgainRec := httptest.NewRecorder()
	mux.ServeHTTP(delAgainRec, delAgainReq)
	if delAgainRec.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want %d", delAgainRec.Code, http.StatusNotFound)
	}
}

func TestDeleteZoneRejectsWrongSession(t *testing.T) {
	mux := testZonesRouter(t)
	session := fmt.Sprintf("session-%s", t.Name())

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/zones", bytes.NewReader(validPolygonBody("Owned")))
	createReq.Header.Set(sessionHeader, session)
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)

	var created flightmodel.Zone
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/zones/"+created.ID, nil)
	delReq.Header.Set(sessionHeader, "a-different-session")
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (wrong session should not see the zone)", delRec.Code, http.StatusNotFound)
	}
}
