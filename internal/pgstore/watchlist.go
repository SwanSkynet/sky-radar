package pgstore

import (
	"context"
	"fmt"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

// InsertWatchlistEntry persists entry, the durable write path for a
// user-tracked aircraft per docs/architecture/data-model.md.
func (s *Store) InsertWatchlistEntry(ctx context.Context, entry flightmodel.WatchlistEntry) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO watchlist_entries (id, icao24, label, created_by_session, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, entry.ID, entry.ICAO24, entry.Label, entry.CreatedBySession, entry.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("pgstore: insert watchlist entry %s: %w", entry.ID, err)
	}
	return nil
}

// ListWatchlistEntriesBySession returns every watchlist entry created by
// session, ordered oldest first, for the session-scoped GET /watchlist
// endpoint.
func (s *Store) ListWatchlistEntriesBySession(ctx context.Context, session string) ([]flightmodel.WatchlistEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, icao24, label, created_by_session, created_at
		FROM watchlist_entries
		WHERE created_by_session = $1
		ORDER BY created_at ASC
	`, session)
	if err != nil {
		return nil, fmt.Errorf("pgstore: list watchlist entries by session: %w", err)
	}
	defer rows.Close()
	return scanWatchlistEntries(rows)
}

// ListAllWatchlistEntries returns every watchlist entry regardless of
// owning session, for the event engine's watchlist matching: a
// watchlist_match event must fire for every user's tracked aircraft, not
// just the requesting session's (see cmd/eventengine/watchlist.go).
func (s *Store) ListAllWatchlistEntries(ctx context.Context) ([]flightmodel.WatchlistEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, icao24, label, created_by_session, created_at
		FROM watchlist_entries
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("pgstore: list all watchlist entries: %w", err)
	}
	defer rows.Close()
	return scanWatchlistEntries(rows)
}

// DeleteWatchlistEntry removes the entry identified by id, scoped to
// session so one session cannot delete another's entry, mirroring
// DeleteZone. It reports whether a row was actually deleted.
func (s *Store) DeleteWatchlistEntry(ctx context.Context, id, session string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM watchlist_entries WHERE id = $1 AND created_by_session = $2
	`, id, session)
	if err != nil {
		return false, fmt.Errorf("pgstore: delete watchlist entry %s: %w", id, err)
	}
	return tag.RowsAffected() > 0, nil
}

func scanWatchlistEntries(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]flightmodel.WatchlistEntry, error) {
	out := make([]flightmodel.WatchlistEntry, 0)
	for rows.Next() {
		var entry flightmodel.WatchlistEntry
		if err := rows.Scan(&entry.ID, &entry.ICAO24, &entry.Label, &entry.CreatedBySession, &entry.CreatedAt); err != nil {
			return nil, fmt.Errorf("pgstore: scan watchlist entry row: %w", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: list watchlist entries: %w", err)
	}
	return out, nil
}
