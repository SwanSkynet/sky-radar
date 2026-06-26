package pgstore

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestInsertFlightHistoryWritesRow(t *testing.T) {
	store := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	recordedAt := time.Now().UTC().Truncate(time.Microsecond)
	altitude := 35000
	speed := 420.5
	heading := 270.0

	rec := FlightHistoryRecord{
		ICAO24:         icao24,
		RecordedAt:     recordedAt,
		Lat:            37.5,
		Lon:            -122.25,
		AltitudeBaroFt: &altitude,
		GroundSpeedKt:  &speed,
		HeadingDeg:     &heading,
		OnGround:       false,
	}

	if err := store.InsertFlightHistory(ctx, rec); err != nil {
		t.Fatalf("InsertFlightHistory: %v", err)
	}

	var gotLat, gotLon float64
	var gotAltitude int
	var gotSpeed, gotHeading float64
	var gotOnGround bool
	row := store.pool.QueryRow(ctx, `
		SELECT lat, lon, altitude_baro_ft, ground_speed_kt, heading_deg, on_ground
		FROM flight_history WHERE icao24 = $1 AND recorded_at = $2
	`, icao24, recordedAt)
	if err := row.Scan(&gotLat, &gotLon, &gotAltitude, &gotSpeed, &gotHeading, &gotOnGround); err != nil {
		t.Fatalf("scan inserted row: %v", err)
	}

	if gotLat != rec.Lat || gotLon != rec.Lon {
		t.Errorf("lat/lon = %v/%v, want %v/%v", gotLat, gotLon, rec.Lat, rec.Lon)
	}
	if gotAltitude != altitude {
		t.Errorf("altitude_baro_ft = %d, want %d", gotAltitude, altitude)
	}
	if gotSpeed != speed {
		t.Errorf("ground_speed_kt = %v, want %v", gotSpeed, speed)
	}
	if gotHeading != heading {
		t.Errorf("heading_deg = %v, want %v", gotHeading, heading)
	}
	if gotOnGround {
		t.Errorf("on_ground = true, want false")
	}
}

func TestQueryFlightHistoryRangeReturnsRowsInWindowOrderedByTime(t *testing.T) {
	store := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	base := time.Now().UTC().Truncate(time.Microsecond)

	// before: outside the requested window (too early).
	// in1/in2: inside the window, deliberately inserted out of order so
	// the test also asserts ascending-by-recorded_at ordering.
	// after: outside the requested window (too late).
	before := FlightHistoryRecord{ICAO24: icao24, RecordedAt: base.Add(-time.Minute), Lat: 0, Lon: 0}
	in2 := FlightHistoryRecord{ICAO24: icao24, RecordedAt: base.Add(20 * time.Second), Lat: 2, Lon: 2}
	in1 := FlightHistoryRecord{ICAO24: icao24, RecordedAt: base.Add(10 * time.Second), Lat: 1, Lon: 1}
	after := FlightHistoryRecord{ICAO24: icao24, RecordedAt: base.Add(time.Hour), Lat: 9, Lon: 9}

	for _, rec := range []FlightHistoryRecord{before, in2, in1, after} {
		if err := store.InsertFlightHistory(ctx, rec); err != nil {
			t.Fatalf("InsertFlightHistory(%v): %v", rec.RecordedAt, err)
		}
	}

	got, err := store.QueryFlightHistoryRange(ctx, base, base.Add(30*time.Second))
	if err != nil {
		t.Fatalf("QueryFlightHistoryRange: %v", err)
	}

	var gotForAircraft []FlightHistoryRecord
	for _, rec := range got {
		if rec.ICAO24 == icao24 {
			gotForAircraft = append(gotForAircraft, rec)
		}
	}
	if len(gotForAircraft) != 2 {
		t.Fatalf("got %d rows for %s, want 2: %+v", len(gotForAircraft), icao24, gotForAircraft)
	}
	if !gotForAircraft[0].RecordedAt.Equal(in1.RecordedAt) || !gotForAircraft[1].RecordedAt.Equal(in2.RecordedAt) {
		t.Errorf("rows not in ascending recorded_at order: %+v", gotForAircraft)
	}
}

func TestQueryFlightHistoryRangeReturnsEmptyWhenNoRowsMatch(t *testing.T) {
	store := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := store.QueryFlightHistoryRange(ctx, time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("QueryFlightHistoryRange: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d rows, want 0", len(got))
	}
}

func TestInsertFlightHistoryIsIdempotentOnConflict(t *testing.T) {
	store := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	recordedAt := time.Now().UTC().Truncate(time.Microsecond)

	rec := FlightHistoryRecord{ICAO24: icao24, RecordedAt: recordedAt, Lat: 1, Lon: 2}

	if err := store.InsertFlightHistory(ctx, rec); err != nil {
		t.Fatalf("InsertFlightHistory (1st): %v", err)
	}
	// A redelivered message for the same (icao24, recorded_at) must not
	// error and must not create a second row.
	if err := store.InsertFlightHistory(ctx, rec); err != nil {
		t.Fatalf("InsertFlightHistory (2nd, duplicate): %v", err)
	}

	var count int
	if err := store.pool.QueryRow(ctx,
		`SELECT count(*) FROM flight_history WHERE icao24 = $1 AND recorded_at = $2`,
		icao24, recordedAt,
	).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1", count)
	}
}
