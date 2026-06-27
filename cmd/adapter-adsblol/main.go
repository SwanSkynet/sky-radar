// Command adapter-adsblol is the source adapter for the adsb.lol provider.
// See docs/api-docs/adsb-lol-api-docs.md and docs/tech-stack/backend.md.
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
	"github.com/SwanSkynet/sky-radar/internal/otelutil"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	serviceName = "adapter-adsblol"
	defaultPort = "8082"

	defaultBaseURL      = "https://api.adsb.lol"
	defaultLat          = 37.6188
	defaultLon          = -122.3758
	defaultRadiusNM     = 250
	defaultPollInterval = 15 * time.Second
	defaultRawStateTTL  = 60 * time.Second
	defaultRedisAddr    = "localhost:6379"
)

// otelMeter and the instruments below are created against otel's global,
// delegating Meter: at this point in program startup otelutil.Init hasn't
// run yet, so they delegate to a no-op implementation (harmless in tests
// that call runPollLoop directly) until main calls otelutil.Init, at which
// point the global MeterProvider swap makes every instrument created here
// start exporting for real. See docs/tech-stack/observability-and-ops.md.
var (
	otelMeter    = otel.Meter(serviceName)
	pollDuration = otelutil.MustFloat64Histogram(otelMeter, "skyradar.adapter.poll.duration",
		metric.WithUnit("s"), metric.WithDescription("Adapter Poll() call latency"))
	pollErrors = otelutil.MustInt64Counter(otelMeter, "skyradar.adapter.poll.errors",
		metric.WithDescription("Adapter Poll() failures"))
	markSourceFresh = otelutil.MustLastSuccessGauge(otelMeter, "skyradar.adapter.source.freshness",
		"Seconds since the last successful poll of this source")
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

	adapter := NewClient(
		&http.Client{Timeout: 10 * time.Second},
		envString("ADSBLOL_BASE_URL", defaultBaseURL),
		envFloat("ADSBLOL_LAT", defaultLat),
		envFloat("ADSBLOL_LON", defaultLon),
		envInt("ADSBLOL_RADIUS_NM", defaultRadiusNM),
	)

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

	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready(redisClient))

	pollInterval := envDuration("POLL_INTERVAL_SECONDS", defaultPollInterval)
	rawStateTTL := envDuration("RAW_STATE_TTL_SECONDS", defaultRawStateTTL)

	go runPollLoop(ctx, logger, adapter, redisClient, pollInterval, rawStateTTL)

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

// runPollLoop polls adapter on a fixed interval, writing each resulting
// RawState to Redis until ctx is done. A failed poll or Redis write is
// logged and skipped rather than stopping the loop, so a transient
// provider/Redis outage doesn't take the adapter down (see P1-FR7).
func runPollLoop(ctx context.Context, logger *slog.Logger, adapter sourceadapter.Adapter, redisClient *redisutil.Client, interval, ttl time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		pollStart := time.Now()
		states, err := adapter.Poll(ctx)
		pollDuration.Record(ctx, time.Since(pollStart).Seconds())
		if err != nil {
			pollErrors.Add(ctx, 1)
			logger.Error("poll failed", "err", err)
		} else {
			markSourceFresh()
			for _, state := range states {
				if err := redisClient.WriteRawState(ctx, state, ttl); err != nil {
					logger.Error("redis write failed", "icao24", state.ICAO24, "err", err)
				}
			}
			logger.Info("poll complete", "count", len(states))
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

func envFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
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
