// Command viewportchurn is the simulated-viewport-churn load-test harness
// required by docs/prd/phase-3-reliability-and-scale.md (P3-FR3/P3-FR4):
// it drives many concurrent WebSocket clients against the real public
// /api/v1/ws endpoint, each with a realistic, continuously panning/
// zooming viewport (see viewport.go), and measures connection success,
// handshake latency, and per-message freshness exactly the way a real
// browser tab would experience them.
//
// It deliberately talks to the public WS contract (cmd/apigateway/
// ws_protocol.go) rather than anything internal, so it's exercising the
// same code path, auth, and rate limiting real traffic goes through.
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
	"sync"
	"syscall"
	"time"

	"github.com/SwanSkynet/sky-radar/loadtest/internal/report"
	"github.com/SwanSkynet/sky-radar/loadtest/internal/stats"
)

func main() {
	var (
		wsURL         = flag.String("ws-url", envOr("WS_URL", "ws://localhost:8080/api/v1/ws"), "WebSocket URL of the public viewport-subscription endpoint")
		apiKey        = flag.String("api-key", os.Getenv("SKYRADAR_API_KEY"), "elevated-tier API key (recommended above ~50 clients; anonymous tier is rate-limited to 60 new connections/min per IP, see cmd/apigateway/auth.go)")
		clients       = flag.Int("clients", 200, "number of concurrent simulated viewport clients")
		rampUp        = flag.Duration("ramp-up", 30*time.Second, "spread client connection starts evenly over this window, instead of opening them all at once")
		churnInterval = flag.Duration("churn-interval", 10*time.Second, "average interval between a client's pan/zoom re-subscriptions")
		duration      = flag.Duration("duration", 5*time.Minute, "how long each connected client stays subscribed")
		dialTimeout   = flag.Duration("dial-timeout", 10*time.Second, "per-client connect+handshake timeout")
		reportPath    = flag.String("report", "", "optional path to write a JSON report (in addition to the stdout summary)")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := clientConfig{
		wsURL:         *wsURL,
		apiKey:        *apiKey,
		churnInterval: *churnInterval,
		duration:      *duration,
		dialTimeout:   *dialTimeout,
	}

	start := time.Now()
	report.PrintHeader(os.Stdout, "viewportchurn", start, fmt.Sprintf(
		"ws_url=%s clients=%d ramp_up=%s churn_interval=%s duration=%s",
		*wsURL, *clients, *rampUp, *churnInterval, *duration))

	samples := make(chan float64, 8192)
	var latencies stats.Latencies
	collectDone := make(chan struct{})
	go func() {
		defer close(collectDone)
		for s := range samples {
			latencies.Add(s)
		}
	}()

	results := make([]clientResult, *clients)
	var wg sync.WaitGroup
	for i := 0; i < *clients; i++ {
		wg.Add(1)
		startDelay := rampDelay(i, *clients, *rampUp)
		go func(idx int, delay time.Duration) {
			defer wg.Done()
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
			results[idx] = runClient(ctx, idx, cfg, samples)
		}(i, startDelay)
	}

	logger.Info("ramping up clients", "clients", *clients, "ramp_up", *rampUp, "run_duration", *duration)
	wg.Wait()
	close(samples)
	<-collectDone

	elapsed := time.Since(start)
	summary := summarize(results)
	percentiles := latencies.Compute()

	fmt.Println()
	fmt.Printf("clients=%d connected=%d failed=%d (success_rate=%.1f%%) total_messages=%d resume_failed=%d wall_clock=%s\n",
		summary.total, summary.connected, summary.failed, summary.successRatePct(), summary.totalMessages, summary.resumeFailed, elapsed.Round(time.Second))
	if summary.handshake.Count > 0 {
		fmt.Printf("handshake latency: count=%d p50=%.2fs p95=%.2fs p99=%.2fs max=%.2fs\n",
			summary.handshake.Count, summary.handshake.P50, summary.handshake.P95, summary.handshake.P99, summary.handshake.Max)
	}
	report.PrintFreshness(os.Stdout, "ws-delivery freshness", percentiles)
	if summary.failed > 0 {
		fmt.Printf("%d/%d clients failed to connect or were dropped — see logs above for the first error per client if -v was used\n", summary.failed, summary.total)
	}

	if *reportPath != "" {
		if err := writeJSONReport(*reportPath, viewportReport{
			StartedAt:        start.UTC(),
			Clients:          summary.total,
			Connected:        summary.connected,
			Failed:           summary.failed,
			TotalMessages:    summary.totalMessages,
			HandshakeLatency: summary.handshake,
			Percentiles:      percentiles,
			Verdict:          report.FreshnessVerdict(percentiles.P95),
		}); err != nil {
			logger.Error("write json report failed", "err", err)
		}
	}
}

// rampDelay spreads client i's start evenly across [0, rampUp), so n
// clients ramp up linearly rather than all dialing in the same instant —
// the latter would both understate a real traffic ramp and very likely
// blow through the anonymous-tier per-IP rate limit's burst capacity.
func rampDelay(i, total int, rampUp time.Duration) time.Duration {
	if total <= 1 || rampUp <= 0 {
		return 0
	}
	return rampUp * time.Duration(i) / time.Duration(total)
}

type resultSummary struct {
	total, connected, failed, totalMessages, resumeFailed int
	handshake                                             stats.Percentiles
}

func (s resultSummary) successRatePct() float64 {
	if s.total == 0 {
		return 0
	}
	return float64(s.connected) / float64(s.total) * 100
}

func summarize(results []clientResult) resultSummary {
	var s resultSummary
	var handshakeLatencies stats.Latencies
	s.total = len(results)
	for _, r := range results {
		if r.connectErr != nil {
			s.failed++
			continue
		}
		s.connected++
		s.totalMessages += r.messagesReceived
		s.resumeFailed += r.resumeFailed
		handshakeLatencies.Add(r.handshakeLatency.Seconds())
	}
	s.handshake = handshakeLatencies.Compute()
	return s
}

type viewportReport struct {
	StartedAt        time.Time         `json:"started_at"`
	Clients          int               `json:"clients"`
	Connected        int               `json:"connected"`
	Failed           int               `json:"failed"`
	TotalMessages    int               `json:"total_messages"`
	HandshakeLatency stats.Percentiles `json:"handshake_latency_seconds"`
	Percentiles      stats.Percentiles `json:"freshness_seconds"`
	Verdict          string            `json:"verdict"`
}

func writeJSONReport(path string, r viewportReport) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
