// Command apigateway serves the public REST, GraphQL, and WebSocket API.
// See docs/tech-stack/backend.md.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
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

	// defaultAnonymousRateLimitPerMin and defaultElevatedRateLimitPerMin
	// are the public API v1 tier budgets per
	// docs/prd/phase-2-realtime-systems.md's "API-key auth for elevated
	// rate limits, anonymous tier for casual use". Both are overridable
	// via env vars so they can be tuned without a redeploy of the
	// rate-limiting logic itself, mirroring the event engine's
	// configurable-thresholds rule in docs/tech-stack/backend.md.
	defaultAnonymousRateLimitPerMin = 60
	defaultElevatedRateLimitPerMin  = 600
)

// newRouter wires the public REST and WebSocket routes onto a fresh mux.
// Pulled out of main so tests can exercise the same routing this binary
// actually serves. wsGW, replay, zones, watchlist, and events may be nil in
// tests that don't exercise those paths, in which case their routes are
// simply not registered.
func newRouter(api *flightsAPI, wsGW *wsGateway, replay *replayAPI) http.Handler {
	return newRouterWithExtras(api, wsGW, replay, nil, nil, nil, nil)
}

// apiV1Prefix is the version prefix for every public REST/WebSocket route
// per docs/prd/phase-2-realtime-systems.md's "Public API v1... versioned
// (/api/v1)" requirement. /healthz and /readyz stay unversioned: they're
// infra-level probes, not public API surface, and the openapi schema route
// is served alongside them unversioned-path-wise for discoverability (see
// openapiSpecPath in schema.go).
const apiV1Prefix = "/api/v1"

// newRouterWithExtras is newRouter plus the zones/watchlist/events
// endpoints, split out so existing tests that only need flightsAPI can
// keep calling newRouter without constructing a Postgres-backed Store.
// auth wraps every /api/v1 route in per-key/per-IP rate limiting (see
// auth.go); it may be nil in tests that don't exercise that path, in which
// case requests reach the handlers unthrottled.
func newRouterWithExtras(api *flightsAPI, wsGW *wsGateway, replay *replayAPI, zones *zonesAPI, watchlist *watchlistAPI, events *eventsAPI, auth *apiAuth) http.Handler {
	v1 := http.NewServeMux()
	v1.HandleFunc("GET /flights", api.listFlights)
	v1.HandleFunc("GET /flights/{icao24}", api.getFlight)
	if wsGW != nil {
		v1.HandleFunc("GET /ws", wsGW.handleWS)
	}
	if replay != nil {
		v1.HandleFunc("GET /replay", replay.getReplay)
	}
	if zones != nil {
		v1.HandleFunc("POST /zones", zones.createZone)
		v1.HandleFunc("GET /zones", zones.listZones)
		v1.HandleFunc("DELETE /zones/{id}", zones.deleteZone)
	}
	if watchlist != nil {
		v1.HandleFunc("POST /watchlist", watchlist.createWatchlistEntry)
		v1.HandleFunc("GET /watchlist", watchlist.listWatchlistEntries)
		v1.HandleFunc("DELETE /watchlist/{id}", watchlist.deleteWatchlistEntry)
	}
	if events != nil {
		v1.HandleFunc("GET /events", events.listEvents)
	}

	var v1Handler http.Handler = v1
	if auth != nil {
		v1Handler = auth.middleware(v1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready(api.redis))
	mux.HandleFunc("GET /api/v1/openapi.yaml", serveOpenAPISpec)
	mux.Handle(apiV1Prefix+"/", http.StripPrefix(apiV1Prefix, v1Handler))
	return withCORS(mux)
}

// withCORS allows any origin to use this public API (see
// docs/prd/phase-1-foundation.md: "anonymous-only, generous limits, since
// there's no abuse surface yet" — abuse is now bounded by per-key/per-IP
// rate limiting instead of by blocking cross-origin access), so the
// frontend dev server (different origin/port) can call it directly
// without a proxy. POST/DELETE, X-Session-ID, and apiKeyHeader are allowed
// alongside the original read-only GET routes.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, "+sessionHeader+", "+apiKeyHeader)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	issueKeyLabel := flag.String("issue-key", "", "issue a new public API v1 key with this label, print it, and exit without starting the server")
	issueKeyTier := flag.String("tier", string(tierElevated), "tier for -issue-key: anonymous or elevated")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", serviceName)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Postgres connects first, ahead of Redis/NATS, so -issue-key (an
	// offline admin operation against the durable api_keys table) can
	// exit before touching either of them.
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

	if *issueKeyLabel != "" {
		if _, err := issueAPIKey(ctx, pg, *issueKeyLabel, apiTier(*issueKeyTier)); err != nil {
			logger.Error("issue api key failed", "err", err)
			os.Exit(1)
		}
		return
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
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

	api := &flightsAPI{redis: redisClient, logger: logger}

	auth := &apiAuth{
		pg:             pg,
		redis:          redisClient,
		logger:         logger,
		anonymousLimit: newTierLimit(envInt("ANONYMOUS_RATE_LIMIT_PER_MIN", defaultAnonymousRateLimitPerMin)),
		elevatedLimit:  newTierLimit(envInt("ELEVATED_RATE_LIMIT_PER_MIN", defaultElevatedRateLimitPerMin)),
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
	zonesH := &zonesAPI{pg: pg, logger: logger}
	watchlistH := &watchlistAPI{pg: pg, logger: logger}
	eventsH := &eventsAPI{pg: pg, logger: logger}

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
		Handler: newRouterWithExtras(api, wsGW, replay, zonesH, watchlistH, eventsH, auth),
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

// envInt reads key as an integer, falling back to fallback if it's unset
// or not a valid integer (logged via the package-level default logger
// would require threading one through; an invalid rate-limit env var is
// rare enough config error that failing closed to the documented default
// is simpler and safer than partially wiring a logger just for this).
func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
