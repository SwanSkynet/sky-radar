package pgstore

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	`, event.ID, string(event.Type), strings.ToLower(event.ICAO24), string(event.Severity), event.OccurredAtUTC.UTC(), detail)
	if err != nil {
		return fmt.Errorf("pgstore: insert event %s: %w", event.ID, err)
	}
	return nil
}

// QueryEventsDefaultLimit and QueryEventsMaxLimit bound GET /events: the
// default keeps a typical request small, the max prevents an unbounded
// result set the way QueryFlightHistoryMaxRows does for flight_history.
const (
	QueryEventsDefaultLimit = 100
	QueryEventsMaxLimit     = 1000
)

// EventFilter narrows QueryEvents. Zero-value fields mean "no filter on
// this dimension" except Limit, which is normalized to
// QueryEventsDefaultLimit by QueryEvents when <= 0.
type EventFilter struct {
	Type     flightmodel.EventType
	ICAO24   string
	Severity flightmodel.EventSeverity
	Since    time.Time
	Limit    int
}

// QueryEvents returns events matching filter, most recent first, per the
// "event-type filters" requirement in docs/prd/phase-2-realtime-systems.md.
func (s *Store) QueryEvents(ctx context.Context, filter EventFilter) ([]flightmodel.Event, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = QueryEventsDefaultLimit
	}
	if limit > QueryEventsMaxLimit {
		limit = QueryEventsMaxLimit
	}

	var conditions []string
	var args []any
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if filter.Type != "" {
		conditions = append(conditions, fmt.Sprintf("type = %s", arg(string(filter.Type))))
	}
	if filter.ICAO24 != "" {
		conditions = append(conditions, fmt.Sprintf("icao24 = %s", arg(strings.ToLower(filter.ICAO24))))
	}
	if filter.Severity != "" {
		conditions = append(conditions, fmt.Sprintf("severity = %s", arg(string(filter.Severity))))
	}
	if !filter.Since.IsZero() {
		conditions = append(conditions, fmt.Sprintf("occurred_at_utc >= %s", arg(filter.Since.UTC())))
	}

	query := "SELECT id, type, icao24, severity, occurred_at_utc, detail FROM events"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += fmt.Sprintf(" ORDER BY occurred_at_utc DESC LIMIT %s", arg(limit))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("pgstore: query events: %w", err)
	}
	defer rows.Close()

	out := make([]flightmodel.Event, 0)
	for rows.Next() {
		var event flightmodel.Event
		var eventType, severity string
		var detail []byte
		if err := rows.Scan(&event.ID, &eventType, &event.ICAO24, &severity, &event.OccurredAtUTC, &detail); err != nil {
			return nil, fmt.Errorf("pgstore: scan event row: %w", err)
		}
		event.Type = flightmodel.EventType(eventType)
		event.Severity = flightmodel.EventSeverity(severity)
		event.Detail = detail
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgstore: query events: %w", err)
	}
	return out, nil
}
