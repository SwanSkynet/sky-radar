package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("loadFixture(%s): %v", name, err)
	}
	return data
}

func TestPollMapsFixtureIntoRawStates(t *testing.T) {
	fixture := loadFixture(t, "states_all_response.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/token", "", "", nil)
	states, err := client.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	// The fixture has 3 entries; one has an empty icao24 and must be skipped.
	if len(states) != 2 {
		t.Fatalf("len(states) = %d, want 2", len(states))
	}

	for _, s := range states {
		if s.Provider != providerName {
			t.Errorf("Provider = %q, want %q", s.Provider, providerName)
		}
		if s.ICAO24 == "" {
			t.Error("ICAO24 is empty")
		}
		if s.ICAO24 != strings.ToLower(s.ICAO24) {
			t.Errorf("ICAO24 %q is not lowercase", s.ICAO24)
		}
		if len(s.Payload) == 0 {
			t.Error("Payload is empty")
		}
		var raw []json.RawMessage
		if err := json.Unmarshal(s.Payload, &raw); err != nil {
			t.Errorf("Payload is not a valid JSON array: %v", err)
		}
		if s.FetchedAt.IsZero() {
			t.Error("FetchedAt is zero")
		}
	}

	if states[0].ICAO24 != "3c6444" {
		t.Errorf("states[0].ICAO24 = %q, want %q", states[0].ICAO24, "3c6444")
	}
	if states[1].ICAO24 != "a1b2c3" {
		t.Errorf("states[1].ICAO24 = %q, want %q", states[1].ICAO24, "a1b2c3")
	}
}

func TestPollOmitsBoundingBoxParamsWhenNotConfigured(t *testing.T) {
	var gotURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time":0,"states":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/token", "", "", nil)
	if _, err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if gotURL != "/states/all" {
		t.Errorf("url = %q, want %q", gotURL, "/states/all")
	}
}

func TestPollIncludesBoundingBoxParamsWhenConfigured(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time":0,"states":[]}`))
	}))
	defer server.Close()

	bbox := &BoundingBox{LaMin: 45.8389, LoMin: 5.9962, LaMax: 47.8229, LoMax: 10.5226}
	client := NewClient(server.Client(), server.URL, server.URL+"/token", "", "", bbox)
	if _, err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	want := map[string]string{
		"lamin": "45.8389",
		"lomin": "5.9962",
		"lamax": "47.8229",
		"lomax": "10.5226",
	}
	for k, v := range want {
		if got := gotQuery.Get(k); got != v {
			t.Errorf("query[%q] = %q, want %q", k, got, v)
		}
	}
}

func TestPollSkipsAuthHeaderWhenCredentialsNotConfigured(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time":0,"states":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/token", "", "", nil)
	if _, err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty", gotAuth)
	}
}

func TestPollFetchesAndSendsBearerTokenWhenCredentialsConfigured(t *testing.T) {
	var tokenRequests int32
	var gotAuth string

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenRequests, 1)
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.FormValue("grant_type"); got != "client_credentials" {
			t.Errorf("grant_type = %q, want %q", got, "client_credentials")
		}
		if got := r.FormValue("client_id"); got != "id" {
			t.Errorf("client_id = %q, want %q", got, "id")
		}
		if got := r.FormValue("client_secret"); got != "secret" {
			t.Errorf("client_secret = %q, want %q", got, "secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-token","expires_in":300}`))
	})
	mux.HandleFunc("/states/all", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time":0,"states":[]}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/auth/token", "id", "secret", nil)
	if _, err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-token")
	}
	if tokenRequests != 1 {
		t.Fatalf("tokenRequests = %d, want 1", tokenRequests)
	}
}

func TestTokenIsCachedAcrossPolls(t *testing.T) {
	var tokenRequests int32

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenRequests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"cached-token","expires_in":300}`))
	})
	mux.HandleFunc("/states/all", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time":0,"states":[]}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/auth/token", "id", "secret", nil)
	if _, err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll #1: %v", err)
	}
	if _, err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll #2: %v", err)
	}

	if tokenRequests != 1 {
		t.Fatalf("tokenRequests = %d, want 1 (token should be cached)", tokenRequests)
	}
}

func TestTokenIsRefreshedAfterExpiry(t *testing.T) {
	var tokenRequests int32

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokenRequests, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"expiring-token","expires_in":0}`))
	})
	mux.HandleFunc("/states/all", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time":0,"states":[]}`))
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/auth/token", "id", "secret", nil)
	if _, err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll #1: %v", err)
	}
	if _, err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll #2: %v", err)
	}

	if tokenRequests != 2 {
		t.Fatalf("tokenRequests = %d, want 2 (token should be refreshed once expired)", tokenRequests)
	}
}

func TestPollFailsWhenResponseExceedsSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(make([]byte, maxResponseBodyBytes+1))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/token", "", "", nil)
	if _, err := client.Poll(context.Background()); err == nil {
		t.Fatal("Poll returned nil error, want error on oversized response")
	}
}

func TestPollRetriesOn429ThenSucceeds(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"time":0,"states":[["abc123","X",null,null,0,null,null,null,false,null,null,null,null,null,null,false,0,0]]}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/token", "", "", nil)
	states, err := client.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(states) != 1 || states[0].ICAO24 != "abc123" {
		t.Fatalf("states = %+v, want one RawState for abc123", states)
	}
}

func TestPollFailsAfterExhaustingRetriesOn5xx(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/token", "", "", nil)
	client.backoff = sourceadapter.NewBackoff(time.Millisecond, 5*time.Millisecond)
	client.maxAttempts = 3

	_, err := client.Poll(context.Background())
	if err == nil {
		t.Fatal("Poll returned nil error, want error after exhausting retries")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestPollReturnsErrorOnNonRetryableStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/token", "", "", nil)
	if _, err := client.Poll(context.Background()); err == nil {
		t.Fatal("Poll returned nil error, want error on 403")
	}
}

func TestPollReturnsErrorWhenTokenRequestFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := NewClient(server.Client(), server.URL, server.URL+"/auth/token", "bad-id", "bad-secret", nil)
	if _, err := client.Poll(context.Background()); err == nil {
		t.Fatal("Poll returned nil error, want error when token request fails")
	}
}

func TestStateVectorICAO24(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{"valid", `["3c6444","DLH123",null,null,0,null,null,null,false,null,null,null,null,null,null,false,0,0]`, "3c6444", true},
		{"empty icao24", `["","X",null,null,0,null,null,null,false,null,null,null,null,null,null,false,0,0]`, "", true},
		{"not an array", `{"icao24":"3c6444"}`, "", false},
		{"empty array", `[]`, "", false},
	}
	for _, tc := range cases {
		got, ok := stateVectorICAO24(json.RawMessage(tc.raw))
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: stateVectorICAO24() = (%q, %v), want (%q, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := map[string]time.Duration{
		"":    0,
		"0":   0,
		"5":   5 * time.Second,
		"-1":  0,
		"abc": 0,
	}
	for in, want := range cases {
		if got := parseRetryAfter(in); got != want {
			t.Errorf("parseRetryAfter(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestFormatCoord(t *testing.T) {
	if got := formatCoord(45.8389); got != "45.8389" {
		t.Errorf("formatCoord(45.8389) = %q, want %q", got, "45.8389")
	}
	if got := formatCoord(-10.5); got != "-10.5" {
		t.Errorf("formatCoord(-10.5) = %q, want %q", got, "-10.5")
	}
}
