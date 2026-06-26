package pgstore

import (
	"context"
	"fmt"
	"time"
)

// FlightHistoryRecord is one downsampled position sample for an aircraft,
// matching the flight_history schema in docs/architecture/data-model.md.
// JSON tags match that doc's field names exactly: cmd/apigateway's
// GET /replay serializes these directly as the replay wire format, for
// both Postgres- and JetStream-sourced samples (see replay.go's
// flightStateMessageToHistoryRecord).
type FlightHistoryRecord struct {
	ICAO24         string    `json:"icao24"`
	RecordedAt     time.Time `json:"recorded_at"`
	Lat            float64   `json:"lat"`
	Lon            float64   `json:"lon"`
	AltitudeBaroFt *int      `json:"altitude_baro_ft"`
	GroundSpeedKt  *float64  `json:"ground_speed_kt"`
	HeadingDeg     *float64  `json:"heading_deg"`
	OnGround       bool      `json:"on_ground"`
}

// QueryFlightHistoryMaxRows caps how many rows QueryFlightHistoryRange
// returns. flight_history has no per-request viewport filter pushed down
// to SQL (callers filter by bbox in Go, same as the JetStream replay
// path), so without a cap a wide enough [from, to) window over enough
// aircraft could pull an unbounded result set into memory.
const QueryFlightHistoryMaxRows = 100_000

// QueryFlightHistoryRange returns every flight_history row recorded in
// [from, to], ordered by recorded_at ascending, for reconstructing
// movement older than JetStream's retention window in the replay scrubber
// (see docs/prd/phase-2-realtime-systems.md P2-FR5 and
// docs/tech-stack/data-and-messaging.md's downsampled-history rationale).
func (s *Store) QueryFlightHistoryRange(ctx context.Context, from, to time.Time) ([]FlightHistoryRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT icao24, recorded_at, lat, lon, altitude_baro_ft, ground_speed_kt, heading_deg, on_ground
		FROM flight_history
		WHERE recorded_at >= $1 AND recorded_at <= $2
		ORDER BY recorded_at ASC
		LIMIT $3
	`, from.UTC(), to.UTC(), QueryFlightHistoryMaxRows)
	if err != nil {
		return nil, fmt.Errorf("pgstore: query flight history range: %w", err)
	}
	defer rows.Close()

	var out []FlightHistoryRecord
	for rows.Next() {
		var rec FlightHistoryRecord
		if err := rows.Scan(&rec.ICAO24, &rec.RecordedAt, &rec.Lat, &rec.Lon, &rec.AltitudeBaroFt, &rec.GroundSpeedKt, &rec.HeadingDeg, &rec.OnGround); err != nil {
			return nil, fmt.Errorf("pgstore: scan flight history row: %w", err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: query flight history range: %w", err)
	}
	return out, nil
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
