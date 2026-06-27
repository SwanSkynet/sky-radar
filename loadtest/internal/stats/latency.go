// Package stats computes latency percentiles for the load-test harnesses
// under loadtest/. It is scoped to loadtest/ (Go's internal/ visibility),
// not a general-purpose stats library, since its only job is turning a
// stream of per-message latency samples into the percentiles the harnesses
// compare against the master PRD's SLO table.
package stats

import "sort"

// Latencies collects latency samples (in seconds) from a load-test run and
// computes percentiles over them. Not safe for concurrent use; callers
// collecting samples from multiple goroutines must synchronize their own
// Add calls (see loadtest/ingestvolume and loadtest/viewportchurn, which
// funnel samples through a single aggregating goroutine).
type Latencies struct {
	samples []float64
}

// Add records one latency sample in seconds.
func (l *Latencies) Add(seconds float64) {
	l.samples = append(l.samples, seconds)
}

// Len reports how many samples have been recorded.
func (l *Latencies) Len() int {
	return len(l.samples)
}

// Percentiles is a snapshot of a Latencies sample set's distribution, in
// seconds.
type Percentiles struct {
	Count int
	Min   float64
	P50   float64
	P95   float64
	P99   float64
	Max   float64
}

// Compute sorts the recorded samples and returns their percentile
// breakdown. Sorting on every call (rather than maintaining a running
// order statistic) is deliberate: harness runs call this once at the end
// of a test, not per-sample, so O(n log n) once is cheaper to reason about
// than an online percentile structure for the sample counts these
// harnesses produce (tens of thousands, not millions).
func (l *Latencies) Compute() Percentiles {
	if len(l.samples) == 0 {
		return Percentiles{}
	}

	sorted := make([]float64, len(l.samples))
	copy(sorted, l.samples)
	sort.Float64s(sorted)

	return Percentiles{
		Count: len(sorted),
		Min:   sorted[0],
		P50:   percentileOf(sorted, 0.50),
		P95:   percentileOf(sorted, 0.95),
		P99:   percentileOf(sorted, 0.99),
		Max:   sorted[len(sorted)-1],
	}
}

// percentileOf returns the nearest-rank percentile p (0..1) of an
// already-sorted slice. Nearest-rank (rather than interpolated) is
// sufficient here: the harnesses report percentiles for a pass/fail
// comparison against an SLO threshold, not for statistically precise
// distribution analysis.
func percentileOf(sorted []float64, p float64) float64 {
	if len(sorted) == 1 {
		return sorted[0]
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}
