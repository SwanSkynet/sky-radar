// Command eventengine subscribes to flights.updates on NATS JetStream as an
// independent downstream consumer and evaluates each update against the
// event-detection rule set, publishing detected Events to events.detected.
// See docs/tech-stack/backend.md. Only the stale-signal, altitude-delta,
// speed-delta, and geofence rules are implemented so far; watchlist match
// lands in a later milestone, see docs/implementation-plan.md.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
)

const (
	serviceName = "eventengine"
	defaultPort = "8085"

	defaultNATSURL = "nats://localhost:4222"

	// consumerName is this service's durable JetStream consumer name on
	// the FLIGHTS_UPDATES stream, distinct from any other subscriber (e.g.
	// pgstore-writer, apigateway) so each tracks its own delivery position
	// independently per docs/architecture/system-architecture.md.
	consumerName = "eventengine"

	// defaultStaleThreshold matches flightmodel.StaleThreshold so the
	// stale-signal rule fires at the same point an aircraft would already
	// be displayed as stale, per docs/architecture/data-model.md.
	defaultStaleThreshold = flightmodel.StaleThreshold

	// defaultStaleSweepInterval keeps stale-signal detection latency well
	// within the Phase 2 P95 ≤ 5s event-detection budget (master PRD SLO).
	defaultStaleSweepInterval = 5 * time.Second

	// defaultAltitudeDeltaThresholdFt is the minimum |delta| between two
	// consecutive barometric-altitude readings for the same aircraft that
	// counts as a notable altitude change. 1000ft matches standard IFR
	// altitude separation, so an unexpected jump of at least that much
	// between reports is worth surfacing.
	defaultAltitudeDeltaThresholdFt = 1000

	// defaultAltitudeWarningThresholdFt escalates an altitude_delta event
	// to EventSeverityWarning once the jump triples the notable
	// threshold — large enough to suggest a rapid descent/climb or a bad
	// reading rather than a routine step climb/descent.
	defaultAltitudeWarningThresholdFt = 3000

	// defaultSpeedDeltaThresholdKt is the minimum |delta| between two
	// consecutive ground-speed readings for the same aircraft that counts
	// as a notable speed change between successive flights.updates
	// reports.
	defaultSpeedDeltaThresholdKt = 100

	// defaultSpeedWarningThresholdKt escalates a speed_delta event to
	// EventSeverityWarning once the jump reaches 2.5x the notable
	// threshold — large enough to suggest an abrupt maneuver or a bad
	// reading rather than routine acceleration/deceleration.
	defaultSpeedWarningThresholdKt = 250
)

// geofenceZonesEnvVar names the env var holding the JSON-encoded
// []flightmodel.Zone the geofence detector evaluates against. Loading
// zones from Postgres is durable-store wiring deferred to a later
// milestone (see docs/implementation-plan.md); this env var is the
// smallest input the engine needs to run the rule in the meantime.
const geofenceZonesEnvVar = "GEOFENCE_ZONES_JSON"

func healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", serviceName)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	nc, err := natsutil.Connect(envString("NATS_URL", defaultNATSURL))
	if err != nil {
		logger.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	js, err := natsutil.JetStream(nc)
	if err != nil {
		logger.Error("jetstream context failed", "err", err)
		os.Exit(1)
	}
	if _, err := natsutil.EnsureFlightsUpdatesStream(ctx, js); err != nil {
		logger.Error("ensure flights.updates stream failed", "err", err)
		os.Exit(1)
	}
	if _, err := natsutil.EnsureEventsDetectedStream(ctx, js); err != nil {
		logger.Error("ensure events.detected stream failed", "err", err)
		os.Exit(1)
	}
	subscriber, err := natsutil.NewFlightStateSubscriber(ctx, js, consumerName)
	if err != nil {
		logger.Error("create flights.updates subscriber failed", "err", err)
		os.Exit(1)
	}
	eventPublisher := natsutil.NewEventPublisher(js)

	staleThreshold := envDuration(logger, "STALE_THRESHOLD_SECONDS", defaultStaleThreshold)
	staleDetector := NewStaleSignalDetector(staleThreshold)
	altitudeDetector := NewAltitudeDeltaDetector(
		envInt(logger, "ALTITUDE_DELTA_THRESHOLD_FT", defaultAltitudeDeltaThresholdFt),
		envInt(logger, "ALTITUDE_WARNING_THRESHOLD_FT", defaultAltitudeWarningThresholdFt),
	)
	speedDetector := NewSpeedDeltaDetector(
		envFloat(logger, "SPEED_DELTA_THRESHOLD_KT", defaultSpeedDeltaThresholdKt),
		envFloat(logger, "SPEED_WARNING_THRESHOLD_KT", defaultSpeedWarningThresholdKt),
	)
	zones, err := envZones(geofenceZonesEnvVar)
	if err != nil {
		logger.Error("invalid zones JSON env var", "key", geofenceZonesEnvVar, "err", err)
		os.Exit(1)
	}
	geofenceDetector := NewGeofenceDetector(zones)

	go func() {
		if err := subscriber.Run(ctx, func(err error) {
			logger.Error("decode flight state failed", "err", err)
		}, func(state flightmodel.FlightState) {
			logFlightUpdate(logger, state)
			staleDetector.Observe(state)
			if event, ok := altitudeDetector.Observe(state); ok {
				if err := eventPublisher.PublishEvent(ctx, event); err != nil {
					logger.Error("publish event failed", "icao24", event.ICAO24, "type", event.Type, "err", err)
				} else {
					logger.Info("event detected", "icao24", event.ICAO24, "type", event.Type)
				}
			}
			if event, ok := speedDetector.Observe(state); ok {
				if err := eventPublisher.PublishEvent(ctx, event); err != nil {
					logger.Error("publish event failed", "icao24", event.ICAO24, "type", event.Type, "err", err)
				} else {
					logger.Info("event detected", "icao24", event.ICAO24, "type", event.Type)
				}
			}
			for _, event := range geofenceDetector.Observe(state) {
				if err := eventPublisher.PublishEvent(ctx, event); err != nil {
					logger.Error("publish event failed", "icao24", event.ICAO24, "type", event.Type, "err", err)
				} else {
					logger.Info("event detected", "icao24", event.ICAO24, "type", event.Type)
				}
			}
		}); err != nil {
			// Run only returns a non-nil error if the initial consumer
			// setup fails (it otherwise blocks until ctx is done and
			// returns nil), so this is a startup-class failure: exit
			// rather than leave /healthz reporting ok with no subscriber
			// actually running.
			logger.Error("flights.updates subscriber stopped", "err", err)
			os.Exit(1)
		}
	}()

	staleSweepInterval := envDuration(logger, "STALE_SWEEP_INTERVAL_SECONDS", defaultStaleSweepInterval)
	// speedBaselineEvictAfter mirrors StaleSignalDetector's own
	// evictAfterMultiple bookkeeping: an aircraft is only dropped from
	// speedDetector's baseline map once it has been silent for several
	// multiples of the stale threshold, well past the point it would
	// already be flagged stale.
	speedBaselineEvictAfter := staleThreshold * evictAfterMultiple
	go runStaleSweepLoop(ctx, logger, staleDetector, speedDetector, geofenceDetector, eventPublisher, staleSweepInterval, speedBaselineEvictAfter)

	go func() {
		logger.Info("starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("shutting down")
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
}

// logFlightUpdate records receipt of a flights.updates message. Rule
// evaluation that needs more than a single update in isolation (e.g. the
// altitude-delta and speed-delta rules, evaluated inline in the subscriber
// callback above) hangs off this same callback.
func logFlightUpdate(logger *slog.Logger, state flightmodel.FlightState) {
	var callsign string
	if state.Callsign != nil {
		callsign = *state.Callsign
	}
	logger.Info("received flight update", "icao24", state.ICAO24, "callsign", callsign)
}

// runStaleSweepLoop periodically sweeps detector for aircraft that have
// gone silent past threshold and publishes the resulting Events to
// events.detected until ctx is done. A failed publish is logged and
// skipped rather than stopping the loop, mirroring the normalizer's
// bulkhead handling of transient NATS hiccups (cmd/normalizer/main.go). It
// also evicts speedDetector's and geofenceDetector's per-aircraft state
// once an aircraft has been silent for speedBaselineEvictAfter, bounding
// those detectors' memory growth for aircraft that have gone away rather
// than tracking them for the life of the process.
func runStaleSweepLoop(ctx context.Context, logger *slog.Logger, detector *StaleSignalDetector, speedDetector *SpeedDeltaDetector, geofenceDetector *GeofenceDetector, publisher *natsutil.EventPublisher, interval, speedBaselineEvictAfter time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		now := time.Now()
		for _, event := range detector.Sweep(now) {
			if err := publisher.PublishEvent(ctx, event); err != nil {
				logger.Error("publish event failed", "icao24", event.ICAO24, "type", event.Type, "err", err)
				continue
			}
			logger.Info("event detected", "icao24", event.ICAO24, "type", event.Type)
		}
		speedDetector.EvictBefore(now.Add(-speedBaselineEvictAfter))
		geofenceDetector.EvictBefore(now.Add(-speedBaselineEvictAfter))
	}
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(logger *slog.Logger, key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		logger.Warn("invalid integer env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return n
}

func envFloat(logger *slog.Logger, key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n <= 0 {
		logger.Warn("invalid float env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return n
}

// envZones parses key as a JSON array of flightmodel.Zone, returning nil
// (no zones, geofence detection becomes a no-op) if the env var is unset.
// Malformed JSON is a startup-class failure rather than a silent fallback
// to no zones: with zones still env-sourced (durable Postgres storage is
// out of scope for this milestone, see docs/implementation-plan.md),
// silently ignoring a bad value would leave /healthz reporting ok while the
// configured zone source is actually broken.
func envZones(key string) ([]flightmodel.Zone, error) {
	v := os.Getenv(key)
	if v == "" {
		return nil, nil
	}
	var zones []flightmodel.Zone
	if err := json.Unmarshal([]byte(v), &zones); err != nil {
		return nil, err
	}
	return zones, nil
}

func envDuration(logger *slog.Logger, key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs <= 0 {
		logger.Warn("invalid duration env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return time.Duration(secs) * time.Second
}
