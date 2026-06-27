// Package report prints load-test results in a common format and
// compares them against the freshness SLO in docs/prd/00-master-prd.md,
// so loadtest/ingestvolume and loadtest/viewportchurn (which both
// ultimately measure "how stale is a delivered FlightState") produce
// directly comparable PASS/WARN/FAIL output instead of each inventing its
// own report shape.
package report

import (
	"fmt"
	"io"
	"time"

	"github.com/SwanSkynet/sky-radar/loadtest/internal/stats"
)

// FreshnessTargetSeconds and FreshnessDegradedSeconds are the data
// freshness P95 target and degraded-mode threshold from
// docs/prd/00-master-prd.md's SLO table ("Data freshness (P95) ≤ 15s
// behind source under normal conditions; degraded-mode banner if > 60s").
const (
	FreshnessTargetSeconds   = 15.0
	FreshnessDegradedSeconds = 60.0
)

// FreshnessVerdict classifies a P95 latency against the SLO thresholds.
func FreshnessVerdict(p95Seconds float64) string {
	switch {
	case p95Seconds <= FreshnessTargetSeconds:
		return "PASS"
	case p95Seconds <= FreshnessDegradedSeconds:
		return "WARN (within degraded-mode threshold, above SLO target)"
	default:
		return "FAIL (exceeds degraded-mode threshold)"
	}
}

// PrintFreshness writes a Percentiles breakdown plus its SLO verdict to w,
// labeled by name (e.g. "ingest-to-flights.updates", "ws-delivery").
func PrintFreshness(w io.Writer, name string, p stats.Percentiles) {
	if p.Count == 0 {
		fmt.Fprintf(w, "%s: no samples recorded\n", name)
		return
	}
	fmt.Fprintf(w, "%s: count=%d min=%.2fs p50=%.2fs p95=%.2fs p99=%.2fs max=%.2fs -> %s\n",
		name, p.Count, p.Min, p.P50, p.P95, p.P99, p.Max, FreshnessVerdict(p.P95))
}

// PrintHeader writes a consistent run banner so saved logs are easy to
// scan: tool name, start time, and the config line the caller supplies.
func PrintHeader(w io.Writer, tool string, start time.Time, configLine string) {
	fmt.Fprintf(w, "==== %s ====\nstarted: %s\nconfig: %s\n", tool, start.UTC().Format(time.RFC3339), configLine)
}
