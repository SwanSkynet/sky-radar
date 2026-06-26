package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

// InsertZone persists zone, the durable write path for a user-defined
// geofence per docs/architecture/data-model.md.
func (s *Store) InsertZone(ctx context.Context, zone flightmodel.Zone) error {
	polygon, err := json.Marshal(zone.Polygon)
	if err != nil {
		return fmt.Errorf("pgstore: marshal zone %s polygon: %w", zone.ID, err)
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO zones (id, name, polygon, created_by_session, created_at)
		VALUES ($1, $2, $3::jsonb, $4, $5)
	`, zone.ID, zone.Name, string(polygon), zone.CreatedBySession, zone.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("pgstore: insert zone %s: %w", zone.ID, err)
	}
	return nil
}

// ListZonesBySession returns every zone created by session, ordered oldest
// first, for the session-scoped GET /zones endpoint.
func (s *Store) ListZonesBySession(ctx context.Context, session string) ([]flightmodel.Zone, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, polygon, created_by_session, created_at
		FROM zones
		WHERE created_by_session = $1
		ORDER BY created_at ASC
	`, session)
	if err != nil {
		return nil, fmt.Errorf("pgstore: list zones by session: %w", err)
	}
	defer rows.Close()
	return scanZones(rows)
}

// ListAllZones returns every zone regardless of owning session, for the
// event engine's geofence matching: a geofence_enter/exit event must fire
// against every user's zones, not just the requesting session's (see
// cmd/eventengine/geofence.go).
func (s *Store) ListAllZones(ctx context.Context) ([]flightmodel.Zone, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, polygon, created_by_session, created_at
		FROM zones
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("pgstore: list all zones: %w", err)
	}
	defer rows.Close()
	return scanZones(rows)
}

// DeleteZone removes the zone identified by id, scoped to session so one
// session cannot delete another's zone, mirroring DeleteWatchlistEntry.
// It reports whether a row was actually deleted.
func (s *Store) DeleteZone(ctx context.Context, id, session string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM zones WHERE id = $1 AND created_by_session = $2
	`, id, session)
	if err != nil {
		return false, fmt.Errorf("pgstore: delete zone %s: %w", id, err)
	}
	return tag.RowsAffected() > 0, nil
}

func scanZones(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]flightmodel.Zone, error) {
	out := make([]flightmodel.Zone, 0)
	for rows.Next() {
		var zone flightmodel.Zone
		var polygon []byte
		if err := rows.Scan(&zone.ID, &zone.Name, &polygon, &zone.CreatedBySession, &zone.CreatedAt); err != nil {
			return nil, fmt.Errorf("pgstore: scan zone row: %w", err)
		}
		if err := json.Unmarshal(polygon, &zone.Polygon); err != nil {
			return nil, fmt.Errorf("pgstore: unmarshal zone %s polygon: %w", zone.ID, err)
		}
		out = append(out, zone)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: list zones: %w", err)
	}
	return out, nil
}
