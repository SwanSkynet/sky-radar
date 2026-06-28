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
	"sync"
	"syscall"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/flightmodel"
	"github.com/SwanSkynet/sky-radar/internal/health"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/SwanSkynet/sky-radar/internal/otelutil"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
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

	// mergeConcurrency bounds how many persistAndPublish calls (one Redis
	// pipeline + one JetStream publish-ack each) run at once per merge
	// cycle. The load test in docs/runbooks/load-test.md found that running
	// these strictly sequentially made merge-cycle duration scale linearly
	// with tracked-aircraft count (~0.2ms/flight), which by itself ate a
	// growing share of the 15s merge interval as the fleet grew toward the
	// master PRD's 50,000-entity headroom target. Bounding rather than
	// fully parallelizing avoids opening more concurrent Redis/NATS
	// round-trips than the underlying client pools are sized for.
	mergeConcurrency = 128
)

// otelMeter and the instruments below are created against otel's global,
// delegating Meter: they delegate to a no-op implementation until main
// calls otelutil.Init, which is harmless for tests that call runMergeLoop
// directly. freshness backs the master PRD's "Data freshness (P95) ≤15s
// behind source" SLO directly: it's the gap between a FlightState's source
// observation time (LastSeenUTC) and the moment the normalizer persists
// and publishes it, measured per state rather than once per merge cycle.
var (
	otelMeter = otel.Meter(serviceName)
	freshness = otelutil.MustFloat64Histogram(otelMeter, "skyradar.normalizer.freshness",
		metric.WithUnit("s"), metric.WithDescription("Seconds between a flight state's source observation time and normalizer publish"))
	mergeCycleDuration = otelutil.MustFloat64Histogram(otelMeter, "skyradar.normalizer.merge_cycle.duration",
		metric.WithUnit("s"), metric.WithDescription("Wall-clock time of one merge cycle (Redis scan through publish)"))
	mergeCycleFlights = otelutil.MustInt64Counter(otelMeter, "skyradar.normalizer.merge_cycle.flights",
		metric.WithDescription("Flights produced per merge cycle (recorded as a running total; rate() gives flights/sec)"))
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	providers, err := otelutil.Init(ctx, serviceName, "dev")
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("otel init failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := providers.Shutdown(shutdownCtx); err != nil {
			slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("otel shutdown failed", "err", err)
		}
	}()

	logger := otelutil.NewLogger(serviceName)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", providers.MetricsHandler)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           otelutil.WrapHTTPHandler(serviceName, mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

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

	lastPublished := make(map[string]time.Time)
	for {
		cycleStart := time.Now()
		raws, err := redisClient.ScanRawStates(ctx)
		if err != nil {
			logger.Error("scan raw states failed", "err", err)
		} else {
			states := MergeAll(raws)
			lastPublished = persistAndPublishAll(ctx, logger, redisClient, publisher, states, ttl, lastPublished)
			mergeCycleDuration.Record(ctx, time.Since(cycleStart).Seconds())
			mergeCycleFlights.Add(ctx, int64(len(states)))
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

// persistAndPublishAll runs persistAndPublish for every state concurrently,
// bounded by mergeConcurrency in-flight calls at once, so one merge cycle's
// wall-clock cost approaches one Redis+NATS round trip rather than
// len(states) of them run back to back.
//
// lastPublished maps icao24 to the LastSeenUTC most recently published for
// it; a state whose LastSeenUTC matches is unchanged since the last cycle
// (its raw report hasn't been refreshed) and is written to Redis — so its
// hot-state TTL still gets renewed — but not re-published. raw:* entries
// live for raw-state-ttl (minutes), which spans several merge intervals, so
// without this check every still-current-but-unchanged aircraft would be
// re-merged and re-published on every single cycle: the load test in
// docs/runbooks/load-test.md showed this flooding flights.updates with
// duplicate messages carrying an increasingly stale LastSeenUTC, which both
// wastes Redis/NATS throughput and corrupts the freshness SLO measurement
// (P95 latency tracked the redundant republish gap, not genuine ingest
// latency). The returned map reflects only the states passed in, so an
// aircraft whose raw report has expired and dropped out of states is
// naturally pruned rather than tracked forever.
func persistAndPublishAll(ctx context.Context, logger *slog.Logger, redisClient *redisutil.Client, publisher *natsutil.FlightStatePublisher, states []flightmodel.FlightState, ttl time.Duration, lastPublished map[string]time.Time) map[string]time.Time {
	next := make(map[string]time.Time, len(states))
	var mu sync.Mutex
	sem := make(chan struct{}, mergeConcurrency)
	var wg sync.WaitGroup
	for _, state := range states {
		shouldPublish := !lastPublished[state.ICAO24].Equal(state.LastSeenUTC)

		wg.Add(1)
		sem <- struct{}{}
		go func(state flightmodel.FlightState, shouldPublish bool) {
			defer wg.Done()
			defer func() { <-sem }()
			if persistAndPublish(ctx, logger, redisClient, publisher, state, ttl, shouldPublish) {
				mu.Lock()
				next[state.ICAO24] = state.LastSeenUTC
				mu.Unlock()
			}
		}(state, shouldPublish)
	}
	wg.Wait()
	return next
}

// persistAndPublish writes state to Redis hot state and, only if that
// write succeeds and shouldPublish is true, publishes it to flights.updates.
// Publishing after a failed write would let downstream consumers see an
// update that the /readyz-backing hot state never actually held. It
// reports whether state.LastSeenUTC is now safely reflected as published
// (write succeeded, and publish succeeded if it was attempted) so callers
// only advance their dedup tracking on confirmed success rather than on
// every attempt.
func persistAndPublish(ctx context.Context, logger *slog.Logger, redisClient *redisutil.Client, publisher *natsutil.FlightStatePublisher, state flightmodel.FlightState, ttl time.Duration, shouldPublish bool) bool {
	if err := redisClient.WriteFlightState(ctx, state, ttl); err != nil {
		logger.Error("write flight state failed", "icao24", state.ICAO24, "err", err)
		return false
	}
	if !shouldPublish {
		return true
	}
	freshness.Record(ctx, time.Since(state.LastSeenUTC).Seconds())
	if err := publisher.PublishFlightState(ctx, state); err != nil {
		logger.Error("publish flight state failed", "icao24", state.ICAO24, "err", err)
		return false
	}
	return true
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
