package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

func TestQueryEventsFiltersByType(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Microsecond)

	altitude := flightmodel.Event{
		ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeAltitudeDelta,
		ICAO24: icao24, Severity: flightmodel.EventSeverityNotable, OccurredAtUTC: now,
	}
	speed := flightmodel.Event{
		ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeSpeedDelta,
		ICAO24: icao24, Severity: flightmodel.EventSeverityNotable, OccurredAtUTC: now.Add(time.Second),
	}
	for _, e := range []flightmodel.Event{altitude, speed} {
		if err := store.InsertEvent(ctx, e); err != nil {
			t.Fatalf("InsertEvent(%s): %v", e.Type, err)
		}
	}

	got, err := store.QueryEvents(ctx, EventFilter{Type: flightmodel.EventTypeAltitudeDelta, ICAO24: icao24})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(got) != 1 || got[0].ID != altitude.ID {
		t.Fatalf("got %+v, want exactly the altitude_delta event", got)
	}
}

func TestQueryEventsFiltersByICAO24CaseInsensitive(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icao24 := fmt.Sprintf("T%dAB", time.Now().UnixNano())
	event := flightmodel.Event{
		ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeAltitudeDelta,
		ICAO24: icao24, Severity: flightmodel.EventSeverityNotable, OccurredAtUTC: time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := store.InsertEvent(ctx, event); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	got, err := store.QueryEvents(ctx, EventFilter{ICAO24: strings.ToUpper(icao24)})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(got) != 1 || got[0].ID != event.ID {
		t.Fatalf("got %+v, want exactly the inserted event regardless of icao24 casing", got)
	}
}

func TestQueryEventsFiltersBySeverityAndSince(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	old := flightmodel.Event{
		ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeStaleSignal,
		ICAO24: icao24, Severity: flightmodel.EventSeverityWarning,
		OccurredAtUTC: time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond),
	}
	recentInfo := flightmodel.Event{
		ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeStaleSignal,
		ICAO24: icao24, Severity: flightmodel.EventSeverityInfo,
		OccurredAtUTC: time.Now().UTC().Truncate(time.Microsecond),
	}
	recentWarning := flightmodel.Event{
		ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeStaleSignal,
		ICAO24: icao24, Severity: flightmodel.EventSeverityWarning,
		OccurredAtUTC: time.Now().UTC().Add(time.Second).Truncate(time.Microsecond),
	}
	for _, e := range []flightmodel.Event{old, recentInfo, recentWarning} {
		if err := store.InsertEvent(ctx, e); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
	}

	got, err := store.QueryEvents(ctx, EventFilter{
		ICAO24:   icao24,
		Severity: flightmodel.EventSeverityWarning,
		Since:    time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(got) != 1 || got[0].ID != recentWarning.ID {
		t.Fatalf("got %+v, want exactly the recent warning event", got)
	}
}

func TestQueryEventsOrdersMostRecentFirstAndRespectsLimit(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	icao24 := fmt.Sprintf("t%d", time.Now().UnixNano())
	base := time.Now().UTC().Truncate(time.Microsecond)
	var ids []string
	for i := 0; i < 3; i++ {
		e := flightmodel.Event{
			ID: flightmodel.NewEventID(), Type: flightmodel.EventTypeStaleSignal,
			ICAO24: icao24, Severity: flightmodel.EventSeverityInfo,
			OccurredAtUTC: base.Add(time.Duration(i) * time.Second),
		}
		if err := store.InsertEvent(ctx, e); err != nil {
			t.Fatalf("InsertEvent: %v", err)
		}
		ids = append(ids, e.ID)
	}

	got, err := store.QueryEvents(ctx, EventFilter{ICAO24: icao24, Limit: 2})
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (limit)", len(got))
	}
	if got[0].ID != ids[2] || got[1].ID != ids[1] {
		t.Errorf("got order %v, want most-recent-first %v", []string{got[0].ID, got[1].ID}, []string{ids[2], ids[1]})
	}
}
