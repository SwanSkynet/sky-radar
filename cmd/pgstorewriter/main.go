// Command pgstorewriter consumes flights.updates and events.detected,
// persisting a downsampled flight_history row per aircraft and a durable
// events row per detected Event to Postgres. It is the "pgstore-writer"
// component in docs/architecture/system-architecture.md, kept separate
// from event evaluation (cmd/eventengine) per docs/tech-stack/backend.md.
// Its downsampling bookkeeping is in-memory hot state local to this
// process (see internal/pgstorewriter's package doc), not externalized to
// Redis.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
	"github.com/SwanSkynet/sky-radar/internal/pgstorewriter"
)

const (
	serviceName = "pgstorewriter"
	defaultPort = "8086"

	defaultNATSURL     = "nats://localhost:4222"
	defaultDatabaseURL = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

	// flightsConsumerName and eventsConsumerName are this service's
	// durable JetStream consumer names, distinct from eventengine's and
	// apigateway's own consumer names so each subscriber tracks its own
	// delivery position independently per
	// docs/architecture/system-architecture.md.
	flightsConsumerName = "pgstorewriter-flights"
	eventsConsumerName  = "pgstorewriter-events"

	// defaultHistoryDownsampleInterval matches the "one row per aircraft
	// per ~10s interval" cost/fidelity trade-off documented in
	// docs/tech-stack/data-and-messaging.md.
	defaultHistoryDownsampleInterval = 10 * time.Second

	// evictAfterMultiple mirrors the event engine's bookkeeping-eviction
	// policy (cmd/eventengine/main.go): only drop a per-aircraft
	// downsampling entry once it has been silent for several multiples of
	// the downsample interval, well past the point a normal gap between
	// samples would explain.
	evictAfterMultiple = 6

	pingTimeout = 2 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", serviceName)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := pgstore.Connect(ctx, envString("DATABASE_URL", defaultDatabaseURL))
	if err != nil {
		logger.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		logger.Error("postgres migrate failed", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()
		if err := store.Ping(pingCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unhealthy: postgres unreachable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

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

	flightSub, err := natsutil.NewFlightStateSubscriber(ctx, js, flightsConsumerName)
	if err != nil {
		logger.Error("create flights.updates subscriber failed", "err", err)
		os.Exit(1)
	}
	eventSub, err := natsutil.NewEventSubscriber(ctx, js, eventsConsumerName)
	if err != nil {
		logger.Error("create events.detected subscriber failed", "err", err)
		os.Exit(1)
	}

	downsampleInterval := envDuration(logger, "HISTORY_DOWNSAMPLE_INTERVAL_SECONDS", defaultHistoryDownsampleInterval)
	historyWriter := pgstorewriter.NewHistoryWriter(store, downsampleInterval)
	eventWriter := pgstorewriter.NewEventWriter(store)

	go func() {
		if err := flightSub.Run(ctx, func(err error) {
			logger.Error("decode flight state failed", "err", err)
		}, func(state flightmodel.FlightState) {
			wrote, err := historyWriter.Observe(ctx, state)
			if err != nil {
				logger.Error("write flight history failed", "icao24", state.ICAO24, "err", err)
				return
			}
			if wrote {
				logger.Info("flight history written", "icao24", state.ICAO24)
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

	go func() {
		if err := eventSub.Run(ctx, func(err error) {
			logger.Error("decode event failed", "err", err)
		}, func(event flightmodel.Event) error {
			if err := eventWriter.Observe(ctx, event); err != nil {
				logger.Error("write event failed", "icao24", event.ICAO24, "type", event.Type, "err", err)
				return err
			}
			logger.Info("event written", "icao24", event.ICAO24, "type", event.Type)
			return nil
		}); err != nil {
			logger.Error("events.detected subscriber stopped", "err", err)
			os.Exit(1)
		}
	}()

	evictAfter := downsampleInterval * evictAfterMultiple
	go runEvictionLoop(ctx, historyWriter, downsampleInterval, evictAfter)

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

// runEvictionLoop periodically drops HistoryWriter's per-aircraft
// downsampling bookkeeping for aircraft that have gone silent past
// evictAfter, bounding its memory growth, mirroring the event engine's
// own eviction loop (cmd/eventengine/main.go).
func runEvictionLoop(ctx context.Context, w *pgstorewriter.HistoryWriter, interval, evictAfter time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		w.EvictBefore(time.Now().Add(-evictAfter))
	}
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
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
