package main

import (
	"errors"
	"testing"
	"time"
)

func TestRampDelaySpreadsEvenly(t *testing.T) {
	rampUp := 30 * time.Second
	total := 10
	if got := rampDelay(0, total, rampUp); got != 0 {
		t.Errorf("rampDelay(0,...) = %v, want 0", got)
	}
	last := rampDelay(total-1, total, rampUp)
	if last <= 0 || last >= rampUp {
		t.Errorf("rampDelay(last,...) = %v, want strictly between 0 and %v", last, rampUp)
	}
	// Monotonically non-decreasing across client indices.
	prev := time.Duration(-1)
	for i := 0; i < total; i++ {
		d := rampDelay(i, total, rampUp)
		if d < prev {
			t.Fatalf("rampDelay not monotonic at i=%d: %v < %v", i, d, prev)
		}
		prev = d
	}
}

func TestRampDelayZeroRampUp(t *testing.T) {
	if got := rampDelay(5, 10, 0); got != 0 {
		t.Errorf("rampDelay with zero ramp-up = %v, want 0", got)
	}
}

func TestSummarizeCountsConnectedAndFailed(t *testing.T) {
	results := []clientResult{
		{id: 0, handshakeLatency: 100 * time.Millisecond, messagesReceived: 5},
		{id: 1, connectErr: errors.New("boom")},
		{id: 2, handshakeLatency: 200 * time.Millisecond, messagesReceived: 3, resumeFailed: 1},
	}

	s := summarize(results)
	if s.total != 3 || s.connected != 2 || s.failed != 1 {
		t.Fatalf("summarize() = %+v, want total=3 connected=2 failed=1", s)
	}
	if s.totalMessages != 8 {
		t.Errorf("totalMessages = %d, want 8", s.totalMessages)
	}
	if s.resumeFailed != 1 {
		t.Errorf("resumeFailed = %d, want 1", s.resumeFailed)
	}
	if got := s.successRatePct(); got < 66.0 || got > 67.0 {
		t.Errorf("successRatePct() = %v, want ~66.7", got)
	}
	if s.handshake.Count != 2 {
		t.Errorf("handshake.Count = %d, want 2 (failed connections excluded)", s.handshake.Count)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	s := summarize(nil)
	if s.successRatePct() != 0 {
		t.Errorf("successRatePct() on empty results = %v, want 0", s.successRatePct())
	}
}
