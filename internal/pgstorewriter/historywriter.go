package pgstorewriter

import (
	"context"
	"sync"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
)

// HistoryWriter downsamples flights.updates into flight_history rows,
// writing at most one row per aircraft per Interval. This preserves the
// cost/fidelity trade-off documented in
// docs/tech-stack/data-and-messaging.md: writing every normalized update
// to Postgres would be unnecessary write amplification at this project's
// scale and budget, so only a sample taken at most every Interval per
// icao24 is persisted durably — full-resolution recent data lives in
// Redis/JetStream's own retention window instead.
type HistoryWriter struct {
	store    *pgstore.Store
	interval time.Duration

	mu       sync.Mutex
	lastSeen map[string]time.Time
}

// NewHistoryWriter returns a HistoryWriter that samples at most once per
// interval per aircraft.
func NewHistoryWriter(store *pgstore.Store, interval time.Duration) *HistoryWriter {
	return &HistoryWriter{
		store:    store,
		interval: interval,
		lastSeen: make(map[string]time.Time),
	}
}

// Observe writes a downsampled flight_history row for state if at least
// Interval has elapsed (by state.LastSeenUTC, not wall-clock time) since
// the last row written for this icao24. It returns (false, nil) when the
// update was skipped as part of normal downsampling rather than failed.
//
// The gate check and lastSeen reservation happen under a single lock so
// concurrent calls for the same icao24 cannot both pass the gate and
// double-insert; on insert failure the reservation is rolled back so the
// next call can retry.
func (w *HistoryWriter) Observe(ctx context.Context, state flightmodel.FlightState) (bool, error) {
	icao24 := state.ICAO24
	now := state.LastSeenUTC

	w.mu.Lock()
	last, ok := w.lastSeen[icao24]
	if ok && now.Sub(last) < w.interval {
		w.mu.Unlock()
		return false, nil
	}
	w.lastSeen[icao24] = now
	w.mu.Unlock()

	rec := pgstore.FlightHistoryRecord{
		ICAO24:         icao24,
		RecordedAt:     now,
		Lat:            state.Lat,
		Lon:            state.Lon,
		AltitudeBaroFt: state.AltitudeBaroFt,
		GroundSpeedKt:  state.GroundSpeedKt,
		HeadingDeg:     state.HeadingDeg,
		OnGround:       state.OnGround,
	}
	if err := w.store.InsertFlightHistory(ctx, rec); err != nil {
		w.mu.Lock()
		if w.lastSeen[icao24] == now {
			if ok {
				w.lastSeen[icao24] = last
			} else {
				delete(w.lastSeen, icao24)
			}
		}
		w.mu.Unlock()
		return false, err
	}

	return true, nil
}

// EvictBefore drops downsampling bookkeeping for any aircraft whose last
// written sample is older than cutoff, bounding memory growth for
// aircraft no longer reporting — mirrors the event engine's per-rule
// eviction pattern (see cmd/eventengine/main.go).
func (w *HistoryWriter) EvictBefore(cutoff time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for icao24, last := range w.lastSeen {
		if last.Before(cutoff) {
			delete(w.lastSeen, icao24)
		}
	}
}
