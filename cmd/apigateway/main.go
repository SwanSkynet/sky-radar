// Command apigateway serves the public REST, GraphQL, and WebSocket API.
// See docs/tech-stack/backend.md.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/health"
	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/SwanSkynet/sky-radar/internal/pgstore"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/redis/go-redis/v9"
)

const (
	serviceName = "apigateway"
	defaultPort = "8080"

	defaultRedisAddr   = "localhost:6379"
	defaultNATSURL     = "nats://localhost:4222"
	defaultDatabaseURL = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
)

// newRouter wires the public REST and WebSocket routes onto a fresh mux.
// Pulled out of main so tests can exercise the same routing this binary
// actually serves. wsGW and replay may be nil in tests that don't exercise
// those paths, in which case GET /ws and GET /replay are simply not
// registered.
func newRouter(api *flightsAPI, wsGW *wsGateway, replay *replayAPI) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready(api.redis))
	mux.HandleFunc("GET /flights", api.listFlights)
	mux.HandleFunc("GET /flights/{icao24}", api.getFlight)
	if wsGW != nil {
		mux.HandleFunc("GET /ws", wsGW.handleWS)
	}
	if replay != nil {
		mux.HandleFunc("GET /replay", replay.getReplay)
	}
	return withCORS(mux)
}

// withCORS allows any origin to read this anonymous, read-only public API
// (see docs/prd/phase-1-foundation.md: "anonymous-only, generous limits,
// since there's no abuse surface yet"), so the frontend dev server
// (different origin/port) can poll it directly without a proxy.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", serviceName)

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
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

	api := &flightsAPI{redis: redisClient, logger: logger}

	pg, err := pgstore.Connect(ctx, envString("DATABASE_URL", defaultDatabaseURL))
	if err != nil {
		logger.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer pg.Close()

	if err := pg.Migrate(ctx); err != nil {
		logger.Error("postgres migrate failed", "err", err)
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

	hub := newWSHub()
	wsGW := newWSGateway(hub, js, logger)
	replay := &replayAPI{js: js, pg: pg, logger: logger}

	liveTail, err := natsutil.NewFlightStateLiveTailReader(ctx, js)
	if err != nil {
		logger.Error("create flights.updates live tail reader failed", "err", err)
		os.Exit(1)
	}
	go func() {
		if err := liveTail.Run(ctx, func(err error) {
			logger.Error("live tail decode error", "err", err)
		}, hub.broadcast); err != nil {
			logger.Error("flights.updates live tail reader stopped", "err", err)
			os.Exit(1)
		}
	}()

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: newRouter(api, wsGW, replay),
		// ReadTimeout/WriteTimeout are deliberately unset: net/http sets
		// them as fixed deadlines on the underlying connection before the
		// handler runs, and Hijack (which GET /ws relies on for the
		// WebSocket upgrade) carries those deadlines over rather than
		// clearing them — so a non-zero value here would silently kill
		// every WebSocket connection ReadTimeout/WriteTimeout seconds
		// after it was accepted, REST traffic or not. ReadHeaderTimeout
		// still bounds slow/incomplete request headers either way.
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

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

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
