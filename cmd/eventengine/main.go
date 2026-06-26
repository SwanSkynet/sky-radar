// Command eventengine subscribes to flights.updates on NATS JetStream as an
// independent downstream consumer. See docs/tech-stack/backend.md. Rule
// evaluation/event emission is not implemented yet; see
// docs/implementation-plan.md.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	subscriber, err := natsutil.NewFlightStateSubscriber(ctx, js, consumerName)
	if err != nil {
		logger.Error("create flights.updates subscriber failed", "err", err)
		os.Exit(1)
	}

	go func() {
		if err := subscriber.Run(ctx, func(err error) {
			logger.Error("decode flight state failed", "err", err)
		}, func(state flightmodel.FlightState) {
			logFlightUpdate(logger, state)
		}); err != nil {
			logger.Error("flights.updates subscriber stopped", "err", err)
		}
	}()

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

// logFlightUpdate records receipt of a flights.updates message. It is a
// placeholder for the rule evaluation described in
// docs/prd/phase-2-realtime-systems.md, which lands in a later milestone.
func logFlightUpdate(logger *slog.Logger, state flightmodel.FlightState) {
	logger.Info("received flight update", "icao24", state.ICAO24, "callsign", state.Callsign)
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
