package pgstorewriter

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
)

func TestEventWriterPersistsEvent(t *testing.T) {
	store := newTestStore(t)
	writer := NewEventWriter(store)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	event := flightmodel.Event{
		ID:            flightmodel.NewEventID(),
		Type:          flightmodel.EventTypeSpeedDelta,
		ICAO24:        fmt.Sprintf("t%d", time.Now().UnixNano()),
		Severity:      flightmodel.EventSeverityWarning,
		OccurredAtUTC: time.Now().UTC().Truncate(time.Microsecond),
	}

	if err := writer.Observe(ctx, event); err != nil {
		t.Fatalf("Observe: %v", err)
	}

	pool := newTestQueryPool(t)
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE id = $1`, event.ID).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if count != 1 {
		t.Errorf("row count = %d, want 1", count)
	}

	// Every event is written through, with no downsampling, unlike
	// HistoryWriter.
	if err := writer.Observe(ctx, event); err != nil {
		t.Fatalf("Observe (redelivery): %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM events WHERE id = $1`, event.ID).Scan(&count); err != nil {
		t.Fatalf("count rows after redelivery: %v", err)
	}
	if count != 1 {
		t.Errorf("row count after redelivery = %d, want 1", count)
	}
}
