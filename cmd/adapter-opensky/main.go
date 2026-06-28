// Command adapter-opensky is the source adapter for the OpenSky Network
// provider. See docs/api-docs/opensky-api-docs.md and docs/tech-stack/backend.md.
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
	serviceName = "adapter-opensky"
	defaultPort = "8081"

	defaultBaseURL = "https://opensky-network.org/api"
	defaultAuthURL = "https://auth.opensky-network.org/auth/realms/opensky-network/protocol/openid-connect/token"

	// defaultPollInterval is more conservative than the other adapters'
	// since /states/all is rate limited harder for non-owner queries,
	// especially without OAuth2 credentials configured (see
	// docs/api-docs/opensky-api-docs.md).
	defaultPollInterval = 30 * time.Second
	defaultRawStateTTL  = 60 * time.Second
	defaultRedisAddr    = "localhost:6379"

	// writeConcurrency bounds how many WriteRawState calls run at once per
	// poll batch (see redisutil.WriteRawStatesConcurrently's doc comment).
	writeConcurrency = 64
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

	// OPENSKY_CLIENT_ID/OPENSKY_CLIENT_SECRET are optional: /states/all
	// works anonymously, just at a lower rate limit. Leaving either
	// unset polls anonymously rather than failing to start.
	adapter := NewClient(
		&http.Client{Timeout: 10 * time.Second},
		envString("OPENSKY_BASE_URL", defaultBaseURL),
		envString("OPENSKY_AUTH_URL", defaultAuthURL),
		envString("OPENSKY_CLIENT_ID", ""),
		envString("OPENSKY_CLIENT_SECRET", ""),
		envBoundingBox(),
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
			redisClient.WriteRawStatesConcurrently(ctx, states, ttl, writeConcurrency, func(state sourceadapter.RawState, err error) {
				logger.Error("redis write failed", "icao24", state.ICAO24, "err", err)
			})
			logger.Info("poll complete", "count", len(states))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// envBoundingBox builds a BoundingBox from OPENSKY_LAMIN/LOMIN/LAMAX/LOMAX
// when all four are set and parse cleanly, otherwise it returns nil so the
// adapter queries the entire network (see docs/api-docs/opensky-api-docs.md).
func envBoundingBox() *BoundingBox {
	lamin, ok1 := envFloatOK("OPENSKY_LAMIN")
	lomin, ok2 := envFloatOK("OPENSKY_LOMIN")
	lamax, ok3 := envFloatOK("OPENSKY_LAMAX")
	lomax, ok4 := envFloatOK("OPENSKY_LOMAX")
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil
	}
	return &BoundingBox{LaMin: lamin, LoMin: lomin, LaMax: lamax, LoMax: lomax}
}

func envFloatOK(key string) (float64, bool) {
	v := os.Getenv(key)
	if v == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, false
	}
	return f, true
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
