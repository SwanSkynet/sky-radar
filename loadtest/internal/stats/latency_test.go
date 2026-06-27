package stats

import "testing"

func TestComputeEmpty(t *testing.T) {
	var l Latencies
	got := l.Compute()
	if got.Count != 0 {
		t.Fatalf("Count = %d, want 0", got.Count)
	}
}

func TestComputeSingle(t *testing.T) {
	var l Latencies
	l.Add(3.0)
	got := l.Compute()
	if got.Count != 1 || got.Min != 3.0 || got.P50 != 3.0 || got.P95 != 3.0 || got.Max != 3.0 {
		t.Fatalf("Compute() = %+v, want single sample of 3.0 everywhere", got)
	}
}

func TestComputeKnownDistribution(t *testing.T) {
	var l Latencies
	// 1..100 seconds: nearest-rank P50/P95/P99 on a uniform 1..N
	// distribution should land close to N*p.
	for i := 1; i <= 100; i++ {
		l.Add(float64(i))
	}

	got := l.Compute()
	if got.Count != 100 {
		t.Fatalf("Count = %d, want 100", got.Count)
	}
	if got.Min != 1 {
		t.Fatalf("Min = %v, want 1", got.Min)
	}
	if got.Max != 100 {
		t.Fatalf("Max = %v, want 100", got.Max)
	}
	if got.P50 < 49 || got.P50 > 51 {
		t.Fatalf("P50 = %v, want ~50", got.P50)
	}
	if got.P95 < 94 || got.P95 > 96 {
		t.Fatalf("P95 = %v, want ~95", got.P95)
	}
	if got.P99 < 98 || got.P99 > 100 {
		t.Fatalf("P99 = %v, want ~99", got.P99)
	}
}

func TestComputeSmallSample(t *testing.T) {
	// Nearest-rank on tiny samples should pick the upper element for high
	// percentiles rather than rounding down to the lower one.
	var l Latencies
	l.Add(1)
	l.Add(2)
	got := l.Compute()
	if got.P50 != 1 {
		t.Errorf("P50 = %v, want 1 (2 samples)", got.P50)
	}
	if got.P95 != 2 {
		t.Errorf("P95 = %v, want 2 (2 samples)", got.P95)
	}
	if got.P99 != 2 {
		t.Errorf("P99 = %v, want 2 (2 samples)", got.P99)
	}

	var l3 Latencies
	l3.Add(1)
	l3.Add(2)
	l3.Add(3)
	got3 := l3.Compute()
	if got3.P50 != 2 {
		t.Errorf("P50 = %v, want 2 (3 samples)", got3.P50)
	}
	if got3.P95 != 3 {
		t.Errorf("P95 = %v, want 3 (3 samples)", got3.P95)
	}
	if got3.P99 != 3 {
		t.Errorf("P99 = %v, want 3 (3 samples)", got3.P99)
	}
}

func TestComputeUnordered(t *testing.T) {
	var l Latencies
	for _, v := range []float64{5, 1, 4, 2, 3} {
		l.Add(v)
	}
	got := l.Compute()
	if got.Min != 1 || got.Max != 5 || got.P50 != 3 {
		t.Fatalf("Compute() = %+v, want Min=1 Max=5 P50=3", got)
	}
}
