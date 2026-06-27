package otelutil

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// LastSuccessGauge registers an asynchronous gauge named name that reports
// the number of seconds since the most recent call to the returned mark
// function, or -1 if mark has never been called. This is the shared
// "freshness" pattern used by every source adapter, the normalizer, and
// the durable-store writer: a value that must keep decaying between data
// points (e.g. between poll/merge/write cycles), not just snapshot the
// instant a cycle happened to run.
func LastSuccessGauge(meter metric.Meter, name, description string) (mark func(), err error) {
	var lastUnixNano atomic.Int64

	gauge, err := meter.Float64ObservableGauge(name,
		metric.WithDescription(description),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("otelutil: observable gauge %s: %w", name, err)
	}

	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		v := lastUnixNano.Load()
		if v == 0 {
			o.ObserveFloat64(gauge, -1)
			return nil
		}
		o.ObserveFloat64(gauge, time.Since(time.Unix(0, v)).Seconds())
		return nil
	}, gauge)
	if err != nil {
		return nil, fmt.Errorf("otelutil: register callback for %s: %w", name, err)
	}

	return func() { lastUnixNano.Store(time.Now().UnixNano()) }, nil
}

// MustLastSuccessGauge is LastSuccessGauge, panicking on error. Every
// caller registers a fixed, known-good instrument name once at process
// startup (mirroring the regexp.MustCompile/template.Must convention for
// "this can only fail from a programming mistake") — package-level var
// blocks have no error return to propagate to, and failing fast at startup
// is preferable to silently shipping no metric.
func MustLastSuccessGauge(meter metric.Meter, name, description string) func() {
	mark, err := LastSuccessGauge(meter, name, description)
	if err != nil {
		panic(err)
	}
	return mark
}

// MustFloat64Histogram is meter.Float64Histogram, panicking on error; see
// MustLastSuccessGauge for why a panicking variant is appropriate here.
func MustFloat64Histogram(meter metric.Meter, name string, opts ...metric.Float64HistogramOption) metric.Float64Histogram {
	h, err := meter.Float64Histogram(name, opts...)
	if err != nil {
		panic(fmt.Errorf("otelutil: histogram %s: %w", name, err))
	}
	return h
}

// MustInt64Counter is meter.Int64Counter, panicking on error; see
// MustLastSuccessGauge for why a panicking variant is appropriate here.
func MustInt64Counter(meter metric.Meter, name string, opts ...metric.Int64CounterOption) metric.Int64Counter {
	c, err := meter.Int64Counter(name, opts...)
	if err != nil {
		panic(fmt.Errorf("otelutil: counter %s: %w", name, err))
	}
	return c
}

// MustInt64UpDownCounter is meter.Int64UpDownCounter, panicking on error;
// see MustLastSuccessGauge for why a panicking variant is appropriate here.
// Used for instantaneous counts that can both rise and fall (e.g. active
// WebSocket connections), unlike a monotonic Int64Counter.
func MustInt64UpDownCounter(meter metric.Meter, name string, opts ...metric.Int64UpDownCounterOption) metric.Int64UpDownCounter {
	c, err := meter.Int64UpDownCounter(name, opts...)
	if err != nil {
		panic(fmt.Errorf("otelutil: updown counter %s: %w", name, err))
	}
	return c
}
