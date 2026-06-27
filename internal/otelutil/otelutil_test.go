package otelutil

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func TestInitWithoutOTLPEndpointStillExposesMetrics(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	providers, err := Init(context.Background(), "test-service", "test")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() {
		if err := providers.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	}()

	meter := otel.Meter("test-service")
	counter := MustInt64Counter(meter, "test.counter")
	counter.Add(context.Background(), 1)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	providers.MetricsHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "test_counter_total") {
		t.Fatalf("metrics body missing test_counter_total:\n%s", rec.Body.String())
	}
}

func TestLastSuccessGaugeReportsNegativeOneBeforeFirstMark(t *testing.T) {
	providers, err := Init(context.Background(), "test-service-freshness", "test")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() { _ = providers.Shutdown(context.Background()) }()

	meter := otel.Meter("test-service-freshness")
	if _, err := LastSuccessGauge(meter, "test.freshness", "test freshness gauge"); err != nil {
		t.Fatalf("LastSuccessGauge: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	providers.MetricsHandler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "test_freshness_seconds{") || !strings.Contains(body, "} -1") {
		t.Fatalf("expected test_freshness_seconds ... -1 before first mark, got:\n%s", body)
	}
}

func TestLastSuccessGaugeReportsElapsedSecondsAfterMark(t *testing.T) {
	providers, err := Init(context.Background(), "test-service-freshness-marked", "test")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() { _ = providers.Shutdown(context.Background()) }()

	meter := otel.Meter("test-service-freshness-marked")
	mark, err := LastSuccessGauge(meter, "test.freshness.marked", "test freshness gauge")
	if err != nil {
		t.Fatalf("LastSuccessGauge: %v", err)
	}
	mark()
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	providers.MetricsHandler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "test_freshness_marked_seconds{") || strings.Contains(body, "} -1") {
		t.Fatalf("expected a non-negative elapsed value after mark, got:\n%s", body)
	}
}

func TestWrapHTTPHandlerInjectsTraceID(t *testing.T) {
	mux := http.NewServeMux()
	var gotTraceID string
	mux.HandleFunc("GET /hello", func(w http.ResponseWriter, r *http.Request) {
		gotTraceID = trace.SpanContextFromContext(r.Context()).TraceID().String()
		w.WriteHeader(http.StatusOK)
	})

	wrapped := WrapHTTPHandler("test-service", mux)

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotTraceID == "" {
		t.Fatal("expected a non-empty trace ID injected by otelhttp")
	}
}

func TestTraceHandlerAddsTraceAndSpanIDWhenSpanPresent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newTraceHandler(slog.NewJSONHandler(&buf, nil)))

	traceID, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	spanID, _ := trace.SpanIDFromHex("0102030405060708")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	logger.InfoContext(ctx, "hello")

	out := buf.String()
	if !strings.Contains(out, traceID.String()) {
		t.Fatalf("log output missing trace_id:\n%s", out)
	}
	if !strings.Contains(out, spanID.String()) {
		t.Fatalf("log output missing span_id:\n%s", out)
	}
}

func TestTraceHandlerOmitsTraceIDWithoutSpan(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(newTraceHandler(slog.NewJSONHandler(&buf, nil)))

	logger.Info("hello")

	if strings.Contains(buf.String(), "trace_id") {
		t.Fatalf("log output should omit trace_id with no active span:\n%s", buf.String())
	}
}
