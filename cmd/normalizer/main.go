// Command normalizer merges adapter output into the canonical FlightState
// and writes current state to Redis. See docs/tech-stack/backend.md.
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

	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/redis/go-redis/v9"
)

const (
	serviceName = "normalizer"
	defaultPort = "8084"

	defaultRedisAddr = "localhost:6379"

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

	mergeInterval := envDuration("MERGE_INTERVAL_SECONDS", defaultMergeInterval)
	flightStateTTL := envDuration("FLIGHT_STATE_TTL_SECONDS", defaultFlightStateTTL)

	go runMergeLoop(ctx, logger, redisClient, mergeInterval, flightStateTTL)

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
// same-icao24 reports per Merge's precedence rule, and writes the
// resulting canonical FlightState into Redis hot state until ctx is done.
// A failed scan or write is logged and skipped rather than stopping the
// loop, so a transient Redis hiccup doesn't take the normalizer down (see
// P1-FR7's bulkhead principle, applied here to the normalizer itself).
func runMergeLoop(ctx context.Context, logger *slog.Logger, redisClient *redisutil.Client, interval, ttl time.Duration) {
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
			}
			logger.Info("merge cycle complete", "raw_count", len(raws), "flight_count", len(states))
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
