package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SwanSkynet/sky-radar/internal/pgstore"
)

// newAuthTestRouter wires a real apiAuth (miniredis + a real, migrated
// Postgres instance) in front of GET /flights, so these tests exercise the
// actual middleware in main.go's request path rather than a stand-in.
func newAuthTestRouter(t *testing.T, anonymousPerMin, elevatedPerMin int) (http.Handler, *pgstore.Store) {
	t.Helper()
	api, redisClient := testAPI(t)
	pg := testReplayPostgres(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	auth := &apiAuth{
		pg:             pg,
		redis:          redisClient,
		logger:         logger,
		anonymousLimit: newTierLimit(anonymousPerMin),
		elevatedLimit:  newTierLimit(elevatedPerMin),
	}
	return newRouterWithExtras(api, nil, nil, nil, nil, nil, auth), pg
}

func doFlightsRequest(mux http.Handler, remoteAddr string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/flights?bbox=-123,36,-121,38", nil)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAnonymousRequestAllowedUnderLimit(t *testing.T) {
	mux, _ := newAuthTestRouter(t, 2, 60)

	rec := doFlightsRequest(mux, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "2" {
		t.Fatalf("X-RateLimit-Limit = %q, want %q", got, "2")
	}
}

func TestAnonymousRequestDeniedOverLimitWithRetryAfter(t *testing.T) {
	mux, _ := newAuthTestRouter(t, 1, 60)

	if rec := doFlightsRequest(mux, "203.0.113.1:1", nil); rec.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", rec.Code)
	}

	rec := doFlightsRequest(mux, "203.0.113.1:1", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("Retry-After header missing on 429 response")
	}
}

func TestDistinctIPsGetIndependentAnonymousBuckets(t *testing.T) {
	mux, _ := newAuthTestRouter(t, 1, 60)

	if rec := doFlightsRequest(mux, "203.0.113.1:1", nil); rec.Code != http.StatusOK {
		t.Fatalf("ip1 request status = %d, want 200", rec.Code)
	}
	if rec := doFlightsRequest(mux, "203.0.113.2:1", nil); rec.Code != http.StatusOK {
		t.Fatalf("ip2 request status = %d, want 200 (independent bucket from ip1)", rec.Code)
	}
}

func TestClientIPPrefersForwardedForHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 192.0.2.1")

	if got := clientIP(req); got != "203.0.113.5" {
		t.Fatalf("clientIP = %q, want %q", got, "203.0.113.5")
	}
}

func TestClientIPFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.1:1234"

	if got := clientIP(req); got != "192.0.2.1" {
		t.Fatalf("clientIP = %q, want %q", got, "192.0.2.1")
	}
}

func TestValidAPIKeyUsesElevatedTierAndOwnBucket(t *testing.T) {
	mux, pg := newAuthTestRouter(t, 1, 5)

	rawKey, err := issueAPIKey(context.Background(), pg, "test-elevated", tierElevated)
	if err != nil {
		t.Fatalf("issueAPIKey: %v", err)
	}

	// Exhaust the anonymous IP bucket first, then prove the keyed request
	// still succeeds on its own, separate elevated-tier bucket.
	if rec := doFlightsRequest(mux, "203.0.113.9:1", nil); rec.Code != http.StatusOK {
		t.Fatalf("anonymous warm-up request status = %d, want 200", rec.Code)
	}
	if rec := doFlightsRequest(mux, "203.0.113.9:1", nil); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("anonymous bucket should now be exhausted, status = %d", rec.Code)
	}

	rec := doFlightsRequest(mux, "203.0.113.9:1", map[string]string{apiKeyHeader: rawKey})
	if rec.Code != http.StatusOK {
		t.Fatalf("keyed request status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "5" {
		t.Fatalf("X-RateLimit-Limit = %q, want %q (elevated tier)", got, "5")
	}
}

func TestInvalidAPIKeyRejected(t *testing.T) {
	mux, _ := newAuthTestRouter(t, 60, 60)

	rec := doFlightsRequest(mux, "", map[string]string{apiKeyHeader: "not-a-real-key"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGenerateAPIKeyIsUniqueAndPrefixed(t *testing.T) {
	a, err := generateAPIKey()
	if err != nil {
		t.Fatalf("generateAPIKey: %v", err)
	}
	b, err := generateAPIKey()
	if err != nil {
		t.Fatalf("generateAPIKey: %v", err)
	}
	if a == b {
		t.Fatal("generateAPIKey produced the same key twice")
	}
	if !strings.HasPrefix(a, "skr_") {
		t.Fatalf("key %q missing skr_ prefix", a)
	}
}

func TestIssueAPIKeyPersistsLookupableKey(t *testing.T) {
	pg := testReplayPostgres(t)

	rawKey, err := issueAPIKey(context.Background(), pg, "test-issue", tierAnonymous)
	if err != nil {
		t.Fatalf("issueAPIKey: %v", err)
	}

	key, err := pg.LookupAPIKeyByHash(context.Background(), hashAPIKey(rawKey))
	if err != nil {
		t.Fatalf("LookupAPIKeyByHash: %v", err)
	}
	if key.Label != "test-issue" || key.Tier != string(tierAnonymous) {
		t.Fatalf("got label=%q tier=%q, want label=%q tier=%q", key.Label, key.Tier, "test-issue", tierAnonymous)
	}
}

func TestIssueAPIKeyRejectsEmptyLabel(t *testing.T) {
	pg := testReplayPostgres(t)

	if _, err := issueAPIKey(context.Background(), pg, "   ", tierElevated); err == nil {
		t.Fatal("issueAPIKey with blank label: want error, got nil")
	}
}

func TestIssueAPIKeyRejectsInvalidTier(t *testing.T) {
	pg := testReplayPostgres(t)

	if _, err := issueAPIKey(context.Background(), pg, "label", apiTier("bogus")); err == nil {
		t.Fatal("issueAPIKey with invalid tier: want error, got nil")
	}
}
