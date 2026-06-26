package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func testEventsRouter(t *testing.T) (http.Handler, *eventsAPI) {
	t.Helper()
	pg := testReplayPostgres(t)
	api, _ := testAPI(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eventsH := &eventsAPI{pg: pg, logger: logger}
	return newRouterWithExtras(api, nil, nil, nil, nil, eventsH, nil), eventsH
}

func TestListEventsFiltersByTypeAndICAO24(t *testing.T) {
	mux, eventsH := testEventsRouter(t)
	ctx := t.Context()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Microsecond)
	altitude := flightmodel.Event{
		ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeAltitudeDelta,
		ICAO24: icao24, Severity: flightmodel.EventSeverityNotable, OccurredAtUTC: now,
	}
	speed := flightmodel.Event{
		ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeSpeedDelta,
		ICAO24: icao24, Severity: flightmodel.EventSeverityNotable, OccurredAtUTC: now.Add(time.Second),
	}
	for _, e := range []flightmodel.Event{altitude, speed} {
		if err := eventsH.pg.InsertEvent(ctx, e); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?type=altitude_delta&icao24="+icao24, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []flightmodel.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 1 || got[0].ID != altitude.ID {
		t.Fatalf("got %+v, want exactly the altitude_delta event", got)
	}
}

func TestListEventsRejectsInvalidType(t *testing.T) {
	mux, _ := testEventsRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?type=not_a_real_type", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestListEventsRejectsInvalidSince(t *testing.T) {
	mux, _ := testEventsRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?since=not-a-timestamp", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestListEventsRespectsLimit(t *testing.T) {
	mux, eventsH := testEventsRouter(t)
	ctx := t.Context()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	base := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 3; i++ {
		e := flightmodel.Event{
			ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeStaleSignal,
			ICAO24: icao24, Severity: flightmodel.EventSeverityInfo,
			OccurredAtUTC: base.Add(time.Duration(i) * time.Second),
		}
		if err := eventsH.pg.InsertEvent(ctx, e); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?icao24="+icao24+"&limit=2", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []flightmodel.Event
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (limit)", len(got))
	}
}
