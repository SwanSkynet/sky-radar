package pgstorewriter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func TestHistoryWriterDownsamplesCadence(t *testing.T) {
	store := newTestStore(t)
	writer := NewHistoryWriter(store, 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	t0 := time.Now().UTC().Truncate(time.Microsecond)

	// First sighting of this aircraft always writes immediately.
	wrote, err := writer.Observe(ctx, flightmodel.FlightState{ICAO24: icao24, LastSeenUTC: t0})
	if err != nil {
		t.Fatalf("Observe (t0): %v", err)
	}
	if !wrote {
		t.Error("Observe (t0): wrote = false, want true (first sighting)")
	}

	// An update only 5s later, within the 10s downsample interval, is
	// skipped.
	wrote, err = writer.Observe(ctx, flightmodel.FlightState{ICAO24: icao24, LastSeenUTC: t0.Add(5 * time.Second)})
	if err != nil {
		t.Fatalf("Observe (t0+5s): %v", err)
	}
	if wrote {
		t.Error("Observe (t0+5s): wrote = true, want false (within downsample interval)")
	}

	// An update 11s after the last *written* sample (t0) has elapsed past
	// the interval and writes again.
	wrote, err = writer.Observe(ctx, flightmodel.FlightState{ICAO24: icao24, LastSeenUTC: t0.Add(11 * time.Second)})
	if err != nil {
		t.Fatalf("Observe (t0+11s): %v", err)
	}
	if !wrote {
		t.Error("Observe (t0+11s): wrote = false, want true (interval elapsed)")
	}

	pool := newTestQueryPool(t)
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM flight_history WHERE icao24 = $1`, icao24,
	).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 2 {
		t.Errorf("row count = %d, want 2 (one per written sample)", count)
	}
}

func TestHistoryWriterTracksAircraftIndependently(t *testing.T) {
	store := newTestStore(t)
	writer := NewHistoryWriter(store, 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	suffix := time.Now().UnixNano()
	icaoA := fmt.Sprintf("a%d", suffix)
	icaoB := fmt.Sprintf("b%d", suffix)
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := writer.Observe(ctx, flightmodel.FlightState{ICAO24: icaoA, LastSeenUTC: now}); err != nil {
		t.Fatalf("Observe icaoA: %v", err)
	}
	wrote, err := writer.Observe(ctx, flightmodel.FlightState{ICAO24: icaoB, LastSeenUTC: now})
	if err != nil {
		t.Fatalf("Observe icaoB: %v", err)
	}
	if !wrote {
		t.Error("Observe icaoB: wrote = false, want true (different aircraft, independent cadence)")
	}
}

func TestHistoryWriterEvictBefore(t *testing.T) {
	store := newTestStore(t)
	writer := NewHistoryWriter(store, 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	t0 := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := writer.Observe(ctx, flightmodel.FlightState{ICAO24: icao24, LastSeenUTC: t0}); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	// Evicting with a cutoff before t0 must not drop the bookkeeping yet.
	writer.EvictBefore(t0.Add(-time.Minute))
	wrote, err := writer.Observe(ctx, flightmodel.FlightState{ICAO24: icao24, LastSeenUTC: t0.Add(5 * time.Second)})
	if err != nil {
		t.Fatalf("Observe after no-op evict: %v", err)
	}
	if wrote {
		t.Error("Observe after no-op evict: wrote = true, want false (bookkeeping should still apply)")
	}

	// Evicting with a cutoff after the last sample drops the bookkeeping,
	// so the next observation writes immediately regardless of interval.
	writer.EvictBefore(t0.Add(time.Minute))
	wrote, err = writer.Observe(ctx, flightmodel.FlightState{ICAO24: icao24, LastSeenUTC: t0.Add(6 * time.Second)})
	if err != nil {
		t.Fatalf("Observe after evict: %v", err)
	}
	if !wrote {
		t.Error("Observe after evict: wrote = false, want true (bookkeeping evicted)")
	}
}
