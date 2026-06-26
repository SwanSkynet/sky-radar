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

	mux := newRouter(api, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights?bbox=-123,36,-121,38", nil)
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
	mux := newRouter(api, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListFlightsRejectsMalformedBBox(t *testing.T) {
	api, _ := testAPI(t)
	mux := newRouter(api, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights?bbox=not-a-bbox", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestListFlightsReturnsEmptyArrayWhenNoneMatch(t *testing.T) {
	api, _ := testAPI(t)
	mux := newRouter(api, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights?bbox=-123,36,-121,38", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "[]\n" {
		t.Errorf("body = %q, want %q", got, "[]\n")
	}
}

func TestListFlightsFiltersByCallsignSubstringCaseInsensitive(t *testing.T) {
	api, redisClient := testAPI(t)
	ctx := t.Context()

	united := "UAL123"
	delta := "DAL456"
	for _, s := range []flightmodel.FlightState{
		{ICAO24: "aaaaaa", Callsign: &united, Lat: 37.0, Lon: -122.0, PositionQuality: flightmodel.PositionQualityADSB, LastSeenUTC: time.Now().UTC()},
		{ICAO24: "bbbbbb", Callsign: &delta, Lat: 37.1, Lon: -122.1, PositionQuality: flightmodel.PositionQualityADSB, LastSeenUTC: time.Now().UTC()},
	} {
		if err := redisClient.WriteFlightState(ctx, s, time.Minute); err != nil {
			t.Fatalf("WriteFlightState(%s): %v", s.ICAO24, err)
		}
	}

	mux := newRouter(api, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights?bbox=-123,36,-121,38&callsign=ual", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got []flightmodel.FlightState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 1 || got[0].ICAO24 != "aaaaaa" {
		t.Fatalf("got %+v, want exactly aaaaaa (callsign filter ual)", got)
	}
}

func TestListFlightsFiltersByAltitudeBand(t *testing.T) {
	api, redisClient := testAPI(t)
	ctx := t.Context()

	low, high := 5000, 35000
	for _, s := range []flightmodel.FlightState{
		{ICAO24: "aaaaaa", AltitudeBaroFt: &low, Lat: 37.0, Lon: -122.0, PositionQuality: flightmodel.PositionQualityADSB, LastSeenUTC: time.Now().UTC()},
		{ICAO24: "bbbbbb", AltitudeBaroFt: &high, Lat: 37.1, Lon: -122.1, PositionQuality: flightmodel.PositionQualityADSB, LastSeenUTC: time.Now().UTC()},
		{ICAO24: "cccccc", Lat: 37.2, Lon: -122.2, PositionQuality: flightmodel.PositionQualityADSB, LastSeenUTC: time.Now().UTC()}, // no altitude reported
	} {
		if err := redisClient.WriteFlightState(ctx, s, time.Minute); err != nil {
			t.Fatalf("WriteFlightState(%s): %v", s.ICAO24, err)
		}
	}

	mux := newRouter(api, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights?bbox=-123,36,-121,38&min_altitude_ft=10000&max_altitude_ft=40000", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got []flightmodel.FlightState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(got) != 1 || got[0].ICAO24 != "bbbbbb" {
		t.Fatalf("got %+v, want exactly bbbbbb (altitude band 10000-40000)", got)
	}
}

func TestListFlightsFiltersByRemainingFields(t *testing.T) {
	reg1, reg2 := "N12345", "N67890"
	speed1, speed2 := 250.0, 450.0
	states := []flightmodel.FlightState{
		{ICAO24: "aaaaaa", Registration: &reg1, GroundSpeedKt: &speed1, Lat: 37.0, Lon: -122.0, PositionQuality: flightmodel.PositionQualityADSB, LastSeenUTC: time.Now().UTC()},
		{ICAO24: "bbbbbb", Registration: &reg2, GroundSpeedKt: &speed2, Lat: 37.1, Lon: -122.1, PositionQuality: flightmodel.PositionQualityADSB, LastSeenUTC: time.Now().UTC()},
	}

	tests := []struct {
		name       string
		query      string
		wantICAO24 string
	}{
		{name: "registration substring case-insensitive", query: "registration=n123", wantICAO24: "aaaaaa"},
		{name: "icao24 substring", query: "icao24=bbb", wantICAO24: "bbbbbb"},
		{name: "min_speed_kt excludes slower aircraft", query: "min_speed_kt=300", wantICAO24: "bbbbbb"},
		{name: "max_speed_kt excludes faster aircraft", query: "max_speed_kt=300", wantICAO24: "aaaaaa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, redisClient := testAPI(t)
			ctx := t.Context()
			for _, s := range states {
				if err := redisClient.WriteFlightState(ctx, s, time.Minute); err != nil {
					t.Fatalf("WriteFlightState(%s): %v", s.ICAO24, err)
				}
			}

			mux := newRouter(api, nil, nil)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/flights?bbox=-123,36,-121,38&"+tt.query, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			var got []flightmodel.FlightState
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if len(got) != 1 || got[0].ICAO24 != tt.wantICAO24 {
				t.Fatalf("got %+v, want exactly %s", got, tt.wantICAO24)
			}
		})
	}
}

func TestListFlightsRejectsMalformedFilterValue(t *testing.T) {
	api, _ := testAPI(t)
	mux := newRouter(api, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights?bbox=-123,36,-121,38&min_altitude_ft=not-a-number", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestListFlightsRejectsNonFiniteSpeedFilter(t *testing.T) {
	for _, value := range []string{"NaN", "Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			api, _ := testAPI(t)
			mux := newRouter(api, nil, nil)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/flights?bbox=-123,36,-121,38&min_speed_kt="+value, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
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

	mux := newRouter(api, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights/A1B2C3", nil)
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
	mux := newRouter(api, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights/ffffff", nil)
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

	mux := newRouter(api, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights/a1b2c3", nil)
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
