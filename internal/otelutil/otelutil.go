package otelutil

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Providers bundles the process-wide OpenTelemetry state Init creates.
// main is expected to register MetricsHandler on its mux and defer
// Shutdown.
type Providers struct {
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	MetricsHandler http.Handler
}

// Init wires the global TracerProvider and MeterProvider for serviceName.
//
// Tracing only ships spans when OTEL_EXPORTER_OTLP_ENDPOINT is set (e.g. to
// a Grafana Alloy/OTel Collector instance forwarding to Grafana Cloud
// Tempo in production); without it, a local dev or self-hosted instance
// stays trace-free instead of endlessly retrying a collector that was
// never meant to exist, per docs/tech-stack/observability-and-ops.md's
// "self-host alternative (lite/local mode)".
//
// Metrics are always exposed via MetricsHandler regardless of shipping
// destination, per that same doc's "every service exposes a Prometheus-
// compatible /metrics endpoint... regardless of where metrics ultimately
// get shipped" requirement — a self-hosted Prometheus can scrape it
// directly, and/or a Grafana Alloy agent can scrape it and remote_write to
// Grafana Cloud.
func Init(ctx context.Context, serviceName, serviceVersion string) (*Providers, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(serviceVersion),
		semconv.DeploymentEnvironment(envString("DEPLOY_ENV", "development")),
	))
	if err != nil {
		return nil, fmt.Errorf("otelutil: merge resource: %w", err)
	}

	tp, err := newTracerProvider(ctx, res)
	if err != nil {
		return nil, fmt.Errorf("otelutil: new tracer provider: %w", err)
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	registry := prometheus.NewRegistry()
	promExporter, err := otelprom.New(otelprom.WithRegisterer(registry))
	if err != nil {
		return nil, fmt.Errorf("otelutil: new prometheus exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res), sdkmetric.WithReader(promExporter))
	otel.SetMeterProvider(mp)

	return &Providers{
		TracerProvider: tp,
		MeterProvider:  mp,
		MetricsHandler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	}, nil
}

func newTracerProvider(ctx context.Context, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		// No span processor attached: spans are still created (so calling
		// code never needs an "is tracing enabled" branch) but are dropped
		// immediately rather than held in an unbounded buffer.
		return sdktrace.NewTracerProvider(sdktrace.WithResource(res)), nil
	}

	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return nil, err
	}
	return sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exp),
	), nil
}

// Shutdown flushes and stops both providers, bounded by ctx's deadline, so
// process shutdown doesn't hang on a slow or unreachable collector.
func (p *Providers) Shutdown(ctx context.Context) error {
	tracerErr := p.TracerProvider.Shutdown(ctx)
	meterErr := p.MeterProvider.Shutdown(ctx)
	if tracerErr != nil {
		tracerErr = fmt.Errorf("otelutil: shutdown tracer provider: %w", tracerErr)
	}
	if meterErr != nil {
		meterErr = fmt.Errorf("otelutil: shutdown meter provider: %w", meterErr)
	}
	return errors.Join(tracerErr, meterErr)
}

// WrapHTTPHandler instruments next with otelhttp so every request gets a
// span (linked to the caller's traceparent header, if present) and is
// counted in the standard OTel HTTP server metrics (request duration,
// in-flight requests) — exported via /metrics with an http_route attribute,
// which backs the per-route API latency SLO panels without each service
// hand-rolling its own request-duration histogram.
func WrapHTTPHandler(serviceName string, next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, serviceName,
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if r.Pattern != "" {
				return r.Method + " " + r.Pattern
			}
			return r.Method + " " + r.URL.Path
		}),
	)
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
