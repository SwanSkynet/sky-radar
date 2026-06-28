package redisutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestClient(t *testing.T) *Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return New(&redis.Options{Addr: mr.Addr()})
}

func TestWriteAndReadRawState(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	want := sourceadapter.RawState{
		Provider:  "airplanes.live",
		ICAO24:    "a1b2c3",
		FetchedAt: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
		Payload:   json.RawMessage(`{"hex":"a1b2c3","flight":"UAL123"}`),
	}

	if err := c.WriteRawState(ctx, want, time.Minute); err != nil {
		t.Fatalf("WriteRawState: %v", err)
	}

	got, err := c.ReadRawState(ctx, want.Provider, want.ICAO24)
	if err != nil {
		t.Fatalf("ReadRawState: %v", err)
	}
	if got.Provider != want.Provider || got.ICAO24 != want.ICAO24 || !got.FetchedAt.Equal(want.FetchedAt) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if string(got.Payload) != string(want.Payload) {
		t.Errorf("payload = %s, want %s", got.Payload, want.Payload)
	}
}

func TestReadRawStateMissingReturnsRedisNil(t *testing.T) {
	c := newTestClient(t)
	_, err := c.ReadRawState(context.Background(), "airplanes.live", "doesnotexist")
	if !errors.Is(err, redis.Nil) {
		t.Fatalf("err = %v, want wrapped redis.Nil", err)
	}
}

func TestWriteRawStateOverwritesPreviousPayload(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	first := sourceadapter.RawState{Provider: "airplanes.live", ICAO24: "a1b2c3", Payload: json.RawMessage(`{"v":1}`)}
	second := sourceadapter.RawState{Provider: "airplanes.live", ICAO24: "a1b2c3", Payload: json.RawMessage(`{"v":2}`)}

	if err := c.WriteRawState(ctx, first, time.Minute); err != nil {
		t.Fatalf("WriteRawState(first): %v", err)
	}
	if err := c.WriteRawState(ctx, second, time.Minute); err != nil {
		t.Fatalf("WriteRawState(second): %v", err)
	}

	got, err := c.ReadRawState(ctx, "airplanes.live", "a1b2c3")
	if err != nil {
		t.Fatalf("ReadRawState: %v", err)
	}
	if string(got.Payload) != string(second.Payload) {
		t.Errorf("payload = %s, want %s (overwrite expected)", got.Payload, second.Payload)
	}
}

func TestWriteRawStateRejectsMissingProviderOrICAO24(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	cases := []sourceadapter.RawState{
		{Provider: "", ICAO24: "a1b2c3"},
		{Provider: "airplanes.live", ICAO24: ""},
		{Provider: "  ", ICAO24: "  "},
	}
	for _, state := range cases {
		if err := c.WriteRawState(ctx, state, time.Minute); err == nil {
			t.Errorf("WriteRawState(%+v) returned nil error, want validation error", state)
		}
	}
}

func TestWriteRawStatesConcurrentlyWritesEveryState(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	const n = 200
	states := make([]sourceadapter.RawState, n)
	for i := range states {
		states[i] = sourceadapter.RawState{
			Provider: "airplanes.live",
			ICAO24:   fmt.Sprintf("a%05x", i),
			Payload:  json.RawMessage(`{"v":1}`),
		}
	}

	var mu sync.Mutex
	var errs []error
	c.WriteRawStatesConcurrently(ctx, states, time.Minute, 16, func(_ sourceadapter.RawState, err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})
	if len(errs) != 0 {
		t.Fatalf("onError called %d times, want 0: %v", len(errs), errs)
	}

	for _, s := range states {
		if _, err := c.ReadRawState(ctx, s.Provider, s.ICAO24); err != nil {
			t.Errorf("ReadRawState(%s): %v", s.ICAO24, err)
		}
	}
}

func TestWriteRawStatesConcurrentlyReportsValidationErrors(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	states := []sourceadapter.RawState{
		{Provider: "airplanes.live", ICAO24: "a1b2c3"},
		{Provider: "", ICAO24: "bad"},
	}

	var mu sync.Mutex
	var failed []string
	c.WriteRawStatesConcurrently(ctx, states, time.Minute, 4, func(s sourceadapter.RawState, _ error) {
		mu.Lock()
		failed = append(failed, s.ICAO24)
		mu.Unlock()
	})

	if len(failed) != 1 || failed[0] != "bad" {
		t.Errorf("failed = %v, want exactly [bad]", failed)
	}
}

func TestRawStateKeyFormat(t *testing.T) {
	got := RawStateKey("airplanes.live", "a1b2c3")
	want := "raw:airplanes.live:a1b2c3"
	if got != want {
		t.Errorf("RawStateKey = %q, want %q", got, want)
	}
}

func TestScanRawStatesReturnsEveryProviderAndAircraft(t *testing.T) {
	c := newTestClient(t)
	ctx := context.Background()

	states := []sourceadapter.RawState{
		{Provider: "opensky", ICAO24: "aaaaaa", Payload: json.RawMessage(`{"v":1}`)},
		{Provider: "adsb.lol", ICAO24: "aaaaaa", Payload: json.RawMessage(`{"v":2}`)},
		{Provider: "airplanes.live", ICAO24: "bbbbbb", Payload: json.RawMessage(`{"v":3}`)},
	}
	for _, s := range states {
		if err := c.WriteRawState(ctx, s, time.Minute); err != nil {
			t.Fatalf("WriteRawState(%+v): %v", s, err)
		}
	}

	got, err := c.ScanRawStates(ctx)
	if err != nil {
		t.Fatalf("ScanRawStates: %v", err)
	}
	if len(got) != len(states) {
		t.Fatalf("ScanRawStates returned %d states, want %d: %+v", len(got), len(states), got)
	}
}

func TestScanRawStatesReturnsEmptyWhenNoneWritten(t *testing.T) {
	c := newTestClient(t)

	got, err := c.ScanRawStates(context.Background())
	if err != nil {
		t.Fatalf("ScanRawStates: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ScanRawStates returned %d states, want 0", len(got))
	}
}
