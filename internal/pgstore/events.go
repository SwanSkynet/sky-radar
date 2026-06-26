package pgstore

import (
	"context"
	"fmt"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

// InsertEvent persists event, doing nothing if a row with the same ID
// already exists. The event engine generates a fresh ID per detected
// occurrence, so a duplicate ID here means a JetStream message was
// redelivered after a crash/restart, not a genuine second event.
func (s *Store) InsertEvent(ctx context.Context, event flightmodel.Event) error {
	var detail any
	if len(event.Detail) > 0 {
		detail = string(event.Detail)
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO events (id, type, icao24, severity, occurred_at_utc, detail)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		ON CONFLICT (id) DO NOTHING
	`, event.ID, string(event.Type), event.ICAO24, string(event.Severity), event.OccurredAtUTC.UTC(), detail)
	if err != nil {
		return fmt.Errorf("pgstore: insert event %s: %w", event.ID, err)
	}
	return nil
}
