package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/geo"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
	"github.com/nats-io/nats.go/jetstream"
)

// replayMaxWindow bounds how far back a single GET /replay request can
// span. JetStream's FLIGHTS_UPDATES retention alone only guarantees
// natsutil.FlightsUpdatesMaxAge; Postgres flight_history extends that, but
// an unbounded window over a (possibly bbox-unfiltered) query would let
// one request pull an unbounded number of rows/messages into memory. A
// var, not a const, so tests can shrink it instead of seeding a window
// this large.
var replayMaxWindow = 2 * time.Hour

const (
	// replayJetStreamFetchTimeout bounds how long a replay read waits for
	// JetStream to deliver the requested backlog. PendingCount has already
	// told the caller the exact backlog size up front, so this is a safety
	// bound a healthy fetch should never approach, not a tuning knob.
	replayJetStreamFetchTimeout = 5 * time.Second

	// replayTimeLayout is the query-parameter timestamp format for
	// GET /replay's from/to parameters.
	replayTimeLayout = time.RFC3339
)

// replayAPI holds the dependencies for GET /replay: it reconstructs a
// window of recent movement from JetStream's retained flights.updates
// (full resolution, up to natsutil.FlightsUpdatesMaxAge old) and Postgres
// flight_history (downsampled, for the part of the window older than
// that still within retention), per
// docs/prd/phase-2-realtime-systems.md P2-FR5 and the data source split
// documented in docs/tech-stack/data-and-messaging.md.
type replayAPI struct {
	js     jetstream.JetStream
	pg     *pgstore.Store
	logger *slog.Logger
}

// getReplay handles GET /replay?from=<RFC3339>&to=<RFC3339>[&bbox=...].
// The response is a flat, time-ascending list of flight_history-shaped
// samples (see docs/architecture/data-model.md) regardless of which
// backing store supplied each one, so the frontend scrubber doesn't need
// to know or care where a given sample came from.
func (a *replayAPI) getReplay(w http.ResponseWriter, r *http.Request) {
	from, to, err := parseReplayWindow(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var bbox *geo.BBox
	if bboxParam := r.URL.Query().Get("bbox"); bboxParam != "" {
		parsed, err := geo.ParseBBox(bboxParam)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		bbox = &parsed
	}

	samples, err := a.collectReplaySamples(r.Context(), from, to, bbox)
	if err != nil {
		a.logger.Error("collect replay samples failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to build replay window")
		return
	}

	writeJSON(w, http.StatusOK, samples)
}

// parseReplayWindow validates the from/to query parameters, returning an
// error describing exactly what's wrong with them (used as the 400 body).
func parseReplayWindow(fromParam, toParam string) (time.Time, time.Time, error) {
	if fromParam == "" || toParam == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("from and to query parameters are required (RFC3339 timestamps)")
	}
	from, err := time.Parse(replayTimeLayout, fromParam)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("from must be an RFC3339 timestamp")
	}
	to, err := time.Parse(replayTimeLayout, toParam)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("to must be an RFC3339 timestamp")
	}
	from, to = from.UTC(), to.UTC()
	if !to.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("to must be after from")
	}
	if to.Sub(from) > replayMaxWindow {
		return time.Time{}, time.Time{}, fmt.Errorf("replay window must not exceed %s", replayMaxWindow)
	}
	return from, to, nil
}

// collectReplaySamples splits [from, to] at the JetStream retention
// boundary: the older portion (if any) comes from Postgres
// flight_history, the newer portion from a JetStream replay reader. The
// two portions are disjoint and each individually time-ascending, so
// appending Postgres's (older) results before JetStream's (newer) ones is
// already a correctly time-ordered result — no merge step needed.
func (a *replayAPI) collectReplaySamples(ctx context.Context, from, to time.Time, bbox *geo.BBox) ([]pgstore.FlightHistoryRecord, error) {
	retentionCutoff := time.Now().UTC().Add(-natsutil.FlightsUpdatesMaxAge)

	var samples []pgstore.FlightHistoryRecord

	if from.Before(retentionCutoff) {
		historyTo := to
		if historyTo.After(retentionCutoff) {
			historyTo = retentionCutoff
		}
		history, err := a.pg.QueryFlightHistoryRange(ctx, from, historyTo)
		if err != nil {
			return nil, fmt.Errorf("query flight history: %w", err)
		}
		samples = append(samples, filterByBBox(history, bbox)...)
	}

	streamFrom := from
	if streamFrom.Before(retentionCutoff) {
		streamFrom = retentionCutoff
	}
	if streamFrom.Before(to) {
		streamSamples, err := a.collectJetStreamSamples(ctx, streamFrom, to, bbox)
		if err != nil {
			return nil, fmt.Errorf("collect jetstream replay: %w", err)
		}
		samples = append(samples, streamSamples...)
	}

	return samples, nil
}

// collectJetStreamSamples replays flights.updates from from through to.
// It sizes its single FetchBacklog call exactly via PendingCount rather
// than guessing a batch size, the same precise-sizing approach
// replayResume (ws_gateway.go) uses via FlightsUpdatesHeadSequence.
func (a *replayAPI) collectJetStreamSamples(ctx context.Context, from, to time.Time, bbox *geo.BBox) ([]pgstore.FlightHistoryRecord, error) {
	reader, err := natsutil.NewFlightStateReplayReader(ctx, a.js, from)
	if err != nil {
		return nil, err
	}
	pending, err := reader.PendingCount(ctx)
	if err != nil {
		return nil, err
	}
	if pending == 0 {
		return nil, nil
	}

	backlog, err := reader.FetchBacklog(int(pending), replayJetStreamFetchTimeout, func(decodeErr error) {
		a.logger.Error("replay backlog decode error", "err", decodeErr)
	})
	if err != nil {
		return nil, err
	}

	out := make([]pgstore.FlightHistoryRecord, 0, len(backlog))
	for _, msg := range backlog {
		if msg.Timestamp.After(to) {
			break // ordered delivery: every later message is also past `to`.
		}
		if bbox != nil && !bbox.Contains(msg.State.Lat, msg.State.Lon) {
			continue
		}
		out = append(out, flightStateMessageToHistoryRecord(msg))
	}
	return out, nil
}

// flightStateMessageToHistoryRecord narrows a full canonical FlightState
// (as carried on flights.updates) down to the flight_history field shape,
// so GET /replay returns the same sample shape regardless of which store
// answered a given part of the window.
func flightStateMessageToHistoryRecord(msg natsutil.FlightStateMessage) pgstore.FlightHistoryRecord {
	return pgstore.FlightHistoryRecord{
		ICAO24:         msg.State.ICAO24,
		RecordedAt:     msg.Timestamp,
		Lat:            msg.State.Lat,
		Lon:            msg.State.Lon,
		AltitudeBaroFt: msg.State.AltitudeBaroFt,
		GroundSpeedKt:  msg.State.GroundSpeedKt,
		HeadingDeg:     msg.State.HeadingDeg,
		OnGround:       msg.State.OnGround,
	}
}

func filterByBBox(records []pgstore.FlightHistoryRecord, bbox *geo.BBox) []pgstore.FlightHistoryRecord {
	if bbox == nil {
		return records
	}
	out := make([]pgstore.FlightHistoryRecord, 0, len(records))
	for _, rec := range records {
		if bbox.Contains(rec.Lat, rec.Lon) {
			out = append(out, rec)
		}
	}
	return out
}
