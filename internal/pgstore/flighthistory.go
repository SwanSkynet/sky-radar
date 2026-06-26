package pgstore

import (
	"context"
	"fmt"
	"time"
)

// FlightHistoryRecord is one downsampled position sample for an aircraft,
// matching the flight_history schema in docs/architecture/data-model.md.
type FlightHistoryRecord struct {
	ICAO24         string
	RecordedAt     time.Time
	Lat            float64
	Lon            float64
	AltitudeBaroFt *int
	GroundSpeedKt  *float64
	HeadingDeg     *float64
	OnGround       bool
}

// InsertFlightHistory writes rec, doing nothing if a row already exists
// for (icao24, recorded_at). The pgstorewriter's downsampling already
// prevents duplicate writes in normal operation, but a JetStream message
// redelivered after a crash/restart should not fail the consumer.
func (s *Store) InsertFlightHistory(ctx context.Context, rec FlightHistoryRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO flight_history (icao24, recorded_at, lat, lon, altitude_baro_ft, ground_speed_kt, heading_deg, on_ground)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (icao24, recorded_at) DO NOTHING
	`, rec.ICAO24, rec.RecordedAt.UTC(), rec.Lat, rec.Lon, rec.AltitudeBaroFt, rec.GroundSpeedKt, rec.HeadingDeg, rec.OnGround)
	if err != nil {
		return fmt.Errorf("pgstore: insert flight history %s: %w", rec.ICAO24, err)
	}
	return nil
}
