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
	if gotOnGround {
		t.Errorf("on_ground = true, want false")
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
