package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func TestInsertEventWritesRow(t *testing.T) {
	store := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	event := flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeAltitudeDelta,
		ICAO24:        fmt.Sprintf("t%d", time.Now().UnixNano()),
		Severity:      flightmodel.EventSeverityNotable,
		OccurredAtUTC: time.Now().UTC().Truncate(time.Microsecond),
		Detail:        json.RawMessage(`{"delta_ft":3200}`),
	}

	if err := store.InsertEvent(ctx, event); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	var gotType, gotICAO24, gotSeverity string
	var gotOccurredAt time.Time
	var gotDetail []byte
	row := store.pool.QueryRow(ctx, `
		SELECT type, icao24, severity, occurred_at_utc, detail FROM events WHERE id = $1
	`, event.ID)
	if err := row.Scan(&gotType, &gotICAO24, &gotSeverity, &gotOccurredAt, &gotDetail); err != nil {
		t.Fatalf("scan inserted row: %v", err)
	}

	if gotType != string(event.Type) {
		t.Errorf("type = %q, want %q", gotType, event.Type)
	}
	if gotICAO24 != event.ICAO24 {
		t.Errorf("icao24 = %q, want %q", gotICAO24, event.ICAO24)
	}
	if gotSeverity != string(event.Severity) {
		t.Errorf("severity = %q, want %q", gotSeverity, event.Severity)
	}
	if !gotOccurredAt.Equal(event.OccurredAtUTC) {
		t.Errorf("occurred_at_utc = %v, want %v", gotOccurredAt, event.OccurredAtUTC)
	}

	var detail struct {
		DeltaFt int `json:"delta_ft"`
	}
	if err := json.Unmarshal(gotDetail, &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail.DeltaFt != 3200 {
		t.Errorf("detail.delta_ft = %d, want 3200", detail.DeltaFt)
	}
}

func TestInsertEventIsIdempotentOnConflict(t *testing.T) {
	store := newTestStore(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	event := flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeStaleSignal,
		ICAO24:        fmt.Sprintf("t%d", time.Now().UnixNano()),
		Severity:      flightmodel.EventSeverityWarning,
		OccurredAtUTC: time.Now().UTC().Truncate(time.Microsecond),
	}

	if err := store.InsertEvent(ctx, event); err != nil {
		t.Fatalf("InsertEvent (1st): %v", err)
	}
	if err := store.InsertEvent(ctx, event); err != nil {
		t.Fatalf("InsertEvent (2nd, duplicate id): %v", err)
	}

	var count int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE id = $1`, event.ID).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1", count)
	}
}
