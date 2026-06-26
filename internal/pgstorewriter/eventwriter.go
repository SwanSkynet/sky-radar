package pgstorewriter

import (
	"context"
	"fmt"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
)

// EventWriter persists every Event delivered from events.detected.
// Unlike HistoryWriter, no downsampling applies: the event engine already
// emits a low-volume, rule-filtered stream (see
// docs/tech-stack/data-and-messaging.md), so each Event is written
// durably.
type EventWriter struct {
	store *pgstore.Store
}

// NewEventWriter returns an EventWriter backed by store.
func NewEventWriter(store *pgstore.Store) *EventWriter {
	return &EventWriter{store: store}
}

// Observe writes event to the durable events table.
func (w *EventWriter) Observe(ctx context.Context, event flightmodel.Event) error {
	if err := w.store.InsertEvent(ctx, event); err != nil {
		return fmt.Errorf("pgstorewriter: write event %s: %w", event.ID, err)
	}
	return nil
}
