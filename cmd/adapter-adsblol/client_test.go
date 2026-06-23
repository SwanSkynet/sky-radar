package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	fixture := loadFixture(t, "lat_lon_dist_response.json")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 50.0379, 8.5622, 250)
	states, err := client.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}

	// The fixture has 3 entries; one is missing "hex" and must be skipped.
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
		var raw map[string]any
		if err := json.Unmarshal(s.Payload, &raw); err != nil {
			t.Errorf("Payload is not valid JSON: %v", err)
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

func TestPollQueriesLatLonDistEndpointWithConfiguredCoords(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ac":[],"msg":"No error","total":0,"ctime":0,"ptime":0}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 25.7617, -80.1918, 50)
	if _, err := client.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}

	want := "/v2/lat/25.7617/lon/-80.1918/dist/50"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestNewClientClampsRadiusToDocumentedRange(t *testing.T) {
	cases := map[int]int{-5: 1, 0: 1, 1: 1, 250: 250, 300: 250}
	for in, want := range cases {
		c := NewClient(http.DefaultClient, "https://example.com", 0, 0, in)
		if c.radiusNM != want {
			t.Errorf("radiusNM for input %d = %d, want %d", in, c.radiusNM, want)
		}
	}
}

func TestPollFailsWhenResponseExceedsSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(make([]byte, maxResponseBodyBytes+1))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 0, 0, 1)
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
		_, _ = w.Write([]byte(`{"ac":[{"hex":"abc123"}],"msg":"No error","total":1,"ctime":0,"ptime":0}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 0, 0, 1)
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

	client := NewClient(server.Client(), server.URL, 0, 0, 1)
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
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, 0, 0, 1)
	if _, err := client.Poll(context.Background()); err == nil {
		t.Fatal("Poll returned nil error, want error on 404")
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
	if got := formatCoord(25.7617); got != "25.7617" {
		t.Errorf("formatCoord(25.7617) = %q, want %q", got, "25.7617")
	}
	if got := formatCoord(-80.1918); got != "-80.1918" {
		t.Errorf("formatCoord(-80.1918) = %q, want %q", got, "-80.1918")
	}
}
