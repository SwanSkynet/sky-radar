package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

// loadOpenAPIDoc parses and validates the exact bytes serveOpenAPISpec
// serves (see schema.go's go:embed), so a contract test failure here means
// the published schema itself is malformed, not just out of sync with the
// handlers.
func loadOpenAPIDoc(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(openAPISpecYAML)
	if err != nil {
		t.Fatalf("load embedded openapi spec: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("embedded openapi spec failed validation: %v", err)
	}
	return doc
}

func openAPIRouter(t *testing.T, doc *openapi3.T) routers.Router {
	t.Helper()
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build router from openapi doc: %v", err)
	}
	return router
}

// assertMatchesSchema sends a request built from method/target/headers/body
// twice — once purely to validate against the OpenAPI schema (router),
// once for real against mux — and asserts both the request and the actual
// response conform to the published schema. This is the contract test
// proper: docs/prd/phase-2-realtime-systems.md P2-FR8 requires "contract
// tests against published schema", and this is what keeps openapi-v1.yaml
// honest about what the handlers actually accept and return.
func assertMatchesSchema(t *testing.T, router routers.Router, mux http.Handler, method, target string, headers map[string]string, body []byte) *httptest.ResponseRecorder {
	t.Helper()

	newReq := func() *http.Request {
		var r *http.Request
		if body != nil {
			r = httptest.NewRequest(method, target, bytes.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, target, nil)
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}

	validateReq := newReq()
	route, pathParams, err := router.FindRoute(validateReq)
	if err != nil {
		t.Fatalf("FindRoute(%s %s): %v", method, target, err)
	}

	reqInput := &openapi3filter.RequestValidationInput{
		Request:    validateReq,
		PathParams: pathParams,
		Route:      route,
		Options:    &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}
	if err := openapi3filter.ValidateRequest(validateReq.Context(), reqInput); err != nil {
		t.Fatalf("request %s %s does not satisfy the published schema: %v", method, target, err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newReq())

	respInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: reqInput,
		Status:                 rec.Code,
		Header:                 rec.Header(),
	}
	respInput.SetBodyBytes(rec.Body.Bytes())
	if err := openapi3filter.ValidateResponse(context.Background(), respInput); err != nil {
		t.Fatalf("response from %s %s does not satisfy the published schema (status %d, body %s): %v",
			method, target, rec.Code, rec.Body.String(), err)
	}
	return rec
}

// newContractTestRouter wires flights, events, zones, and watchlist (every
// /api/v1 path the schema documents except /replay, which needs its own
// JetStream harness — see TestReplayMatchesPublishedSchema) behind the
// real, unmodified newRouterWithExtras, so these tests exercise the same
// routing the binary serves.
func newContractTestRouter(t *testing.T) http.Handler {
	t.Helper()
	api, _ := testAPI(t)
	pg := testReplayPostgres(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	zonesH := &zonesAPI{pg: pg, logger: logger}
	watchlistH := &watchlistAPI{pg: pg, logger: logger}
	eventsH := &eventsAPI{pg: pg, logger: logger}
	return newRouterWithExtras(api, nil, nil, zonesH, watchlistH, eventsH, nil)
}

func TestFlightsMatchPublishedSchema(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	router := openAPIRouter(t, doc)

	api, redisClient := testAPI(t)
	mux := newRouter(api, nil, nil)

	callsign := "UAL123"
	state := flightmodel.FlightState{
		ICAO24:          "a1b2c3",
		Callsign:        &callsign,
		Lat:             37.0,
		Lon:             -122.0,
		Sources:         []string{"adsblol"},
		PositionQuality: flightmodel.PositionQualityADSB,
		LastSeenUTC:     time.Now().UTC(),
	}
	if err := redisClient.WriteFlightState(context.Background(), state, time.Minute); err != nil {
		t.Fatalf("WriteFlightState: %v", err)
	}

	assertMatchesSchema(t, router, mux, http.MethodGet, "/api/v1/flights?bbox=-123,36,-121,38", nil, nil)
	assertMatchesSchema(t, router, mux, http.MethodGet, "/api/v1/flights/"+state.ICAO24, nil, nil)
	assertMatchesSchema(t, router, mux, http.MethodGet, "/api/v1/flights/ffffff", nil, nil) // 404, not currently tracked
}

func TestEventsMatchPublishedSchema(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	router := openAPIRouter(t, doc)
	mux := newContractTestRouter(t)

	assertMatchesSchema(t, router, mux, http.MethodGet, "/api/v1/events", nil, nil)
	assertMatchesSchema(t, router, mux, http.MethodGet, "/api/v1/events?type=altitude_delta", nil, nil)
}

func TestZonesMatchPublishedSchema(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	router := openAPIRouter(t, doc)
	mux := newContractTestRouter(t)

	session := fmt.Sprintf("contract-session-%d", time.Now().UnixNano())
	headers := map[string]string{sessionHeader: session}
	body := zoneRequestBody("Contract Zone")

	rec := assertMatchesSchema(t, router, mux, http.MethodPost, "/api/v1/zones", headers, body)
	zoneID := decodeID(t, rec.Body.Bytes())

	assertMatchesSchema(t, router, mux, http.MethodGet, "/api/v1/zones", headers, nil)
	assertMatchesSchema(t, router, mux, http.MethodDelete, "/api/v1/zones/"+zoneID, headers, nil)
}

func TestWatchlistMatchesPublishedSchema(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	router := openAPIRouter(t, doc)
	mux := newContractTestRouter(t)

	session := fmt.Sprintf("contract-session-%d", time.Now().UnixNano())
	headers := map[string]string{sessionHeader: session}
	body := []byte(`{"icao24":"a1b2c3","label":"Contract Entry"}`)

	rec := assertMatchesSchema(t, router, mux, http.MethodPost, "/api/v1/watchlist", headers, body)
	entryID := decodeID(t, rec.Body.Bytes())

	assertMatchesSchema(t, router, mux, http.MethodGet, "/api/v1/watchlist", headers, nil)
	assertMatchesSchema(t, router, mux, http.MethodDelete, "/api/v1/watchlist/"+entryID, headers, nil)
}

func TestReplayMatchesPublishedSchema(t *testing.T) {
	doc := loadOpenAPIDoc(t)
	router := openAPIRouter(t, doc)

	api, _, _ := testReplayAPI(t)
	mux := newRouter(&flightsAPI{logger: api.logger}, nil, api)

	from := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)
	assertMatchesSchema(t, router, mux, http.MethodGet, fmt.Sprintf("/api/v1/replay?from=%s&to=%s", from, to), nil, nil)
}

func zoneRequestBody(name string) []byte {
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

func decodeID(t *testing.T, body []byte) string {
	t.Helper()
	var decoded struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode id from %s: %v", body, err)
	}
	if decoded.ID == "" {
		t.Fatalf("response %s has no id field", body)
	}
	return decoded.ID
}
