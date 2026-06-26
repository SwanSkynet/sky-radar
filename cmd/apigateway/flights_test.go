package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func testAPI(t *testing.T) (*flightsAPI, *redisutil.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	redisClient := redisutil.New(&redis.Options{Addr: mr.Addr()})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &flightsAPI{redis: redisClient, logger: logger}, redisClient
}

func TestListFlightsReturnsAircraftInsideBBox(t *testing.T) {
	api, redisClient := testAPI(t)
	ctx := t.Context()

	callsign := "UAL123"
	inside := flightmodel.FlightState{
		ICAO24:          "aaaaaa",
		Callsign:        &callsign,
		Lat:             37.0,
		Lon:             -122.0,
		PositionQuality: flightmodel.PositionQualityADSB,
		LastSeenUTC:     time.Now().UTC(),
	}
	outside := flightmodel.FlightState{
		ICAO24:          "bbbbbb",
		Lat:             50.0,
		Lon:             -100.0,
		PositionQuality: flightmodel.PositionQualityADSB,
		LastSeenUTC:     time.Now().UTC(),
	}
	for _, s := range []flightmodel.FlightState{inside, outside} {
		if err := redisClient.WriteFlightState(ctx, s, time.Minute); err != nil {
			t.Fatalf("WriteFlightState(%s): %v", s.ICAO24, err)
		}
	}

	mux := newRouter(api, nil)
	req := httptest.NewRequest(http.MethodGet, "/flights?bbox=-123,36,-121,38", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []flightmodel.FlightState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d flights, want 1: %+v", len(got), got)
	}
	if got[0].ICAO24 != "aaaaaa" {
		t.Errorf("ICAO24 = %q, want aaaaaa", got[0].ICAO24)
	}
}

func TestListFlightsRequiresBBoxParam(t *testing.T) {
	api, _ := testAPI(t)
	mux := newRouter(api, nil)

	req := httptest.NewRequest(http.MethodGet, "/flights", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListFlightsRejectsMalformedBBox(t *testing.T) {
	api, _ := testAPI(t)
	mux := newRouter(api, nil)

	req := httptest.NewRequest(http.MethodGet, "/flights?bbox=not-a-bbox", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListFlightsReturnsEmptyArrayWhenNoneMatch(t *testing.T) {
	api, _ := testAPI(t)
	mux := newRouter(api, nil)

	req := httptest.NewRequest(http.MethodGet, "/flights?bbox=-123,36,-121,38", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Errorf("body = %q, want %q", got, "[]\n")
	}
}

func TestGetFlightReturnsCurrentState(t *testing.T) {
	api, redisClient := testAPI(t)
	ctx := t.Context()

	callsign := "UAL123"
	want := flightmodel.FlightState{
		ICAO24:          "a1b2c3",
		Callsign:        &callsign,
		Lat:             37.6188,
		Lon:             -122.3758,
		PositionQuality: flightmodel.PositionQualityADSB,
		LastSeenUTC:     time.Now().UTC(),
	}
	if err := redisClient.WriteFlightState(ctx, want, time.Minute); err != nil {
		t.Fatalf("WriteFlightState: %v", err)
	}

	mux := newRouter(api, nil)
	req := httptest.NewRequest(http.MethodGet, "/flights/A1B2C3", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got flightmodel.FlightState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.ICAO24 != "a1b2c3" {
		t.Errorf("ICAO24 = %q, want a1b2c3 (lookup should be case-insensitive)", got.ICAO24)
	}
	if got.Callsign == nil || *got.Callsign != "UAL123" {
		t.Errorf("Callsign = %v, want UAL123", got.Callsign)
	}
}

func TestGetFlightReturns404WhenNotTracked(t *testing.T) {
	api, _ := testAPI(t)
	mux := newRouter(api, nil)

	req := httptest.NewRequest(http.MethodGet, "/flights/ffffff", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestGetFlightMarksStaleAircraftStale(t *testing.T) {
	api, redisClient := testAPI(t)
	ctx := t.Context()

	stale := flightmodel.FlightState{
		ICAO24:          "a1b2c3",
		Lat:             1,
		Lon:             1,
		PositionQuality: flightmodel.PositionQualityADSB,
		LastSeenUTC:     time.Now().UTC().Add(-5 * time.Minute),
	}
	if err := redisClient.WriteFlightState(ctx, stale, time.Hour); err != nil {
		t.Fatalf("WriteFlightState: %v", err)
	}

	mux := newRouter(api, nil)
	req := httptest.NewRequest(http.MethodGet, "/flights/a1b2c3", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var got flightmodel.FlightState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.Stale {
		t.Error("Stale = false, want true for a flight last seen 5 minutes ago")
	}
}
