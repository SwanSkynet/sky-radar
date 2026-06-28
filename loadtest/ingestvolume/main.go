// Command ingestvolume is the simulated-ingest-volume load-test harness
// required by docs/prd/phase-3-reliability-and-scale.md (P3-FR3): it
// injects a configurable number of synthetic aircraft directly into the
// raw:* Redis keyspace exactly the way cmd/adapter-opensky,
// cmd/adapter-adsblol, and cmd/adapter-airplaneslive do today (see
// internal/redisutil/rawstate.go — adapters still write raw state to
// Redis rather than to the ingest.raw.<provider> NATS subjects the target
// architecture describes), then measures how long each update takes to
// reach flights.updates, which is the same ingest+normalize path real
// traffic goes through end to end.
//
// This intentionally bypasses the adapters' own HTTP polling against
// OpenSky/adsb.lol/airplanes.live — the point of this harness is to load
// the normalizer and downstream consumers at a controlled, repeatable
// volume, not to hammer a third-party provider's free tier.
//
// See docs/runbooks/load-test.md for usage.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/SwanSkynet/sky-radar/internal/natsutil"
	"github.com/SwanSkynet/sky-radar/internal/redisutil"
	"github.com/SwanSkynet/sky-radar/internal/sourceadapter"
	"github.com/SwanSkynet/sky-radar/loadtest/internal/report"
	"github.com/SwanSkynet/sky-radar/loadtest/internal/stats"
	"github.com/redis/go-redis/v9"
)

// writeConcurrency bounds how many WriteRawState calls run at once per
// injection tick (see redisutil.WriteRawStatesConcurrently's doc comment):
// at fleet sizes approaching the master PRD's 50,000-aircraft headroom
// target, a plain sequential write loop's wall-clock cost ate meaningfully
// into the per-aircraft freshness budget, per docs/runbooks/load-test.md.
const writeConcurrency = 64

func main() {
	var (
		redisAddr      = flag.String("redis-addr", envOr("REDIS_ADDR", "localhost:6379"), "Redis address (same instance the normalizer scans raw:* from)")
		natsURL        = flag.String("nats-url", envOr("NATS_URL", "nats://localhost:4222"), "NATS URL (same instance the normalizer publishes flights.updates to)")
		aircraft       = flag.Int("aircraft", 5000, "number of simulated tracked aircraft (master PRD capacity target: 50000 headroom)")
		updateInterval = flag.Duration("update-interval", 15*time.Second, "per-aircraft update cadence (matches the real adapters' poll interval, see cmd/adapter-adsblol)")
		rawStateTTL    = flag.Duration("raw-state-ttl", 60*time.Second, "Redis TTL on each injected raw state (matches the real adapters' default)")
		multiSourcePct = flag.Float64("multi-source-fraction", 0.3, "fraction of aircraft reported by two providers at once, exercising multi-source merge")
		duration       = flag.Duration("duration", 5*time.Minute, "how long to keep injecting updates")
		grace          = flag.Duration("grace-period", 30*time.Second, "extra time to wait for in-flight merges after injection stops before measuring")
		seed           = flag.Int64("seed", time.Now().UnixNano(), "RNG seed for the simulated fleet (fixed value makes a run reproducible)")
		reportPath     = flag.String("report", "", "optional path to write a JSON report (in addition to the stdout summary)")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	redisClient := redisutil.New(&redis.Options{Addr: *redisAddr})
	defer func() { _ = redisClient.Close() }()
	if err := redisClient.Ping(ctx); err != nil {
		logger.Error("redis ping failed", "err", err)
		os.Exit(1)
	}

	nc, err := natsutil.Connect(*natsURL)
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

	fleet := newAircraftFleet(*aircraft, *multiSourcePct, *seed)

	start := time.Now()
	report.PrintHeader(os.Stdout, "ingestvolume", start, fmt.Sprintf(
		"aircraft=%d update_interval=%s raw_state_ttl=%s multi_source_fraction=%.2f duration=%s seed=%d",
		*aircraft, *updateInterval, *rawStateTTL, *multiSourcePct, *duration, *seed))

	var latencies stats.Latencies
	var injected, delivered int
	covered := make(map[string]bool, len(fleet))
	collected := make(chan natsutil.FlightStateMessage, 4096)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for msg := range collected {
			latencies.Add(time.Since(msg.State.LastSeenUTC).Seconds())
			covered[msg.State.ICAO24] = true
			delivered++
		}
	}()

	tailReader, err := natsutil.NewFlightStateLiveTailReader(ctx, js)
	if err != nil {
		logger.Error("create live tail reader failed", "err", err)
		os.Exit(1)
	}

	tailCtx, cancelTail := context.WithCancel(ctx)
	tailDone := make(chan error, 1)
	go func() {
		tailDone <- tailReader.Run(tailCtx, func(err error) {
			logger.Warn("tail decode error", "err", err)
		}, func(msg natsutil.FlightStateMessage) {
			if !strings.HasPrefix(msg.State.ICAO24, syntheticICAO24Block) {
				return // real production traffic sharing this NATS instance; not ours to measure
			}
			collected <- msg
		})
	}()

	logger.Info("injecting synthetic raw states", "aircraft", len(fleet), "interval", *updateInterval)
	injectCtx, cancelInject := context.WithTimeout(ctx, *duration)
	injected = runInjectionLoop(injectCtx, logger, redisClient, fleet, *updateInterval, *rawStateTTL)
	cancelInject()

	logger.Info("injection complete, waiting for in-flight merges", "grace_period", *grace)
	select {
	case <-time.After(*grace):
	case <-ctx.Done():
	}

	cancelTail()
	<-tailDone
	close(collected)
	<-done

	elapsed := time.Since(start)
	percentiles := latencies.Compute()
	coveragePct := float64(len(covered)) / float64(len(fleet)) * 100

	fmt.Println()
	fmt.Printf("raw_states_injected=%d flights_updates_received=%d (%.1f msg/s) aircraft_covered=%d/%d (%.1f%%) wall_clock=%s\n",
		injected, delivered, float64(delivered)/elapsed.Seconds(), len(covered), len(fleet), coveragePct, elapsed.Round(time.Second))
	if coveragePct < 99.0 {
		fmt.Printf("WARNING: %d of %d injected aircraft never appeared on flights.updates — normalizer may be falling behind at this volume, or merge-interval/grace-period is too short for this run length\n",
			len(fleet)-len(covered), len(fleet))
	}
	report.PrintFreshness(os.Stdout, "ingest-to-flights.updates freshness", percentiles)

	if *reportPath != "" {
		if err := writeJSONReport(*reportPath, ingestReport{
			StartedAt:              start.UTC(),
			Aircraft:               *aircraft,
			RawStatesInjected:      injected,
			FlightsUpdatesReceived: delivered,
			AircraftCovered:        len(covered),
			CoveragePct:            coveragePct,
			Percentiles:            percentiles,
			Verdict:                report.FreshnessVerdict(percentiles),
		}); err != nil {
			logger.Error("write json report failed", "err", err)
		}
	}
}

type ingestReport struct {
	StartedAt              time.Time         `json:"started_at"`
	Aircraft               int               `json:"aircraft"`
	RawStatesInjected      int               `json:"raw_states_injected"`
	FlightsUpdatesReceived int               `json:"flights_updates_received"`
	AircraftCovered        int               `json:"aircraft_covered"`
	CoveragePct            float64           `json:"coverage_pct"`
	Percentiles            stats.Percentiles `json:"freshness_seconds"`
	Verdict                string            `json:"verdict"`
}

func writeJSONReport(path string, r ingestReport) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// runInjectionLoop writes one raw state per provider per aircraft, once
// per updateInterval, advancing each aircraft's simulated position first
// so consecutive updates look like a moving flight rather than a
// stationary one. It returns the total number of raw states written.
// Injection failures are logged and skipped (mirroring runMergeLoop's own
// fault-tolerance in cmd/normalizer/main.go) so one bad write doesn't
// abort an otherwise-useful run.
func runInjectionLoop(ctx context.Context, logger *slog.Logger, redisClient *redisutil.Client, fleet []*aircraft, interval, ttl time.Duration) int {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	total := 0
	for {
		now := time.Now()
		var batch []sourceadapter.RawState
		for _, a := range fleet {
			if ctx.Err() != nil {
				// duration elapsed mid-tick: stop writing rather than
				// burning through the rest of the fleet logging a
				// "context deadline exceeded" warning per write — the
				// outer select below handles the actual exit.
				break
			}
			a.step(interval)
			batch = append(batch, a.rawStates(now)...)
		}

		var failed int64
		redisClient.WriteRawStatesConcurrently(ctx, batch, ttl, writeConcurrency, func(raw sourceadapter.RawState, err error) {
			atomic.AddInt64(&failed, 1)
			logger.Warn("write raw state failed", "icao24", raw.ICAO24, "provider", raw.Provider, "err", err)
		})
		total += len(batch) - int(failed)
		logger.Info("injection tick complete", "total_written", total)

		select {
		case <-ctx.Done():
			return total
		case <-ticker.C:
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
