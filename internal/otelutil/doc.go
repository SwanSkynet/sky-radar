// Package otelutil centralizes the OpenTelemetry wiring shared by every
// Sky Radar service: a TracerProvider, a Prometheus-backed MeterProvider
// and its /metrics handler, and a trace-correlated structured logger. See
// docs/tech-stack/observability-and-ops.md.
package otelutil
