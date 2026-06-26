// Command normalizer merges adapter output into the canonical FlightState,
// writes current state to Redis, and publishes each merged update to
// flights.updates on NATS JetStream so downstream consumers (event engine,
// durable-store writer, API gateway) can subscribe independently. See
// docs/tech-stack/backend.md and docs/tech-stack/data-and-messaging.md.
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

	"github.com/SwanSkynet/sky-radar/internal/health"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/redis/go-redis/v9"
)

const (
	serviceName = "normalizer"
	defaultPort = "8084"

	defaultRedisAddr = "localhost:6379"
	defaultNATSURL   = "nats://localhost:4222"

	// defaultMergeInterval matches the adapters' fastest poll cadence (see
	// e.g. cmd/adapter-adsblol) so the normalizer doesn't introduce extra
	// end-to-end latency beyond what ingestion already adds.
	defaultMergeInterval = 15 * time.Second

	// defaultFlightStateTTL is slightly longer than flightmodel.StaleThreshold
	// so a flight:{icao24} key's expiry (meaning "no longer tracked") stays
	// distinct from Stale=true (meaning "tracked but not recently updated"),
	// per docs/tech-stack/data-and-messaging.md.
	defaultFlightStateTTL = 90 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", serviceName)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	mux := http.NewServeMux()

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

	redisClient := redisutil.New(&redis.Options{Addr: envString("REDIS_ADDR", defaultRedisAddr)})
	defer func() {
		if err := redisClient.Close(); err != nil {
			logger.Error("redis close error", "err", err)
		}
	}()

	if err := redisClient.Ping(ctx); err != nil {
		logger.Error("redis ping failed", "err", err)
		os.Exit(1)
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
	publisher := natsutil.NewFlightStatePublisher(js)

	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready(redisClient))

	mergeInterval := envDuration("MERGE_INTERVAL_SECONDS", defaultMergeInterval)
	flightStateTTL := envDuration("FLIGHT_STATE_TTL_SECONDS", defaultFlightStateTTL)

	go runMergeLoop(ctx, logger, redisClient, publisher, mergeInterval, flightStateTTL)

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

// runMergeLoop polls the raw:* keyspace on a fixed interval, merges
// same-icao24 reports per Merge's precedence rule, writes the resulting
// canonical FlightState into Redis hot state, and publishes it to
// flights.updates via publisher until ctx is done. A failed scan, write, or
// publish is logged and skipped rather than stopping the loop, so a
// transient Redis/NATS hiccup doesn't take the normalizer down (see
// P1-FR7's bulkhead principle, applied here to the normalizer itself).
func runMergeLoop(ctx context.Context, logger *slog.Logger, redisClient *redisutil.Client, publisher *natsutil.FlightStatePublisher, interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		raws, err := redisClient.ScanRawStates(ctx)
		if err != nil {
			logger.Error("scan raw states failed", "err", err)
		} else {
			states := MergeAll(raws)
			for _, state := range states {
				if err := redisClient.WriteFlightState(ctx, state, ttl); err != nil {
					logger.Error("write flight state failed", "icao24", state.ICAO24, "err", err)
				}
				if err := publisher.PublishFlightState(ctx, state); err != nil {
					logger.Error("publish flight state failed", "icao24", state.ICAO24, "err", err)
				}
			}
			logger.Info("merge cycle complete", "raw_count", len(raws), "flight_count", len(states))
		}

		if pruned, err := redisClient.PruneExpiredGeoMembers(ctx); err != nil {
			logger.Error("prune expired geo members failed", "err", err)
		} else if pruned > 0 {
			logger.Info("pruned expired geo members", "count", pruned)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return fallback
}
