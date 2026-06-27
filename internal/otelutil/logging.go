package otelutil

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// NewLogger returns the standard slog.Logger every Sky Radar service
// constructs at startup: JSON output to stdout plus a "service" attribute,
// matching what every cmd/* main already built directly. The only addition
// is automatic trace_id/span_id attributes on any log call made with a
// context carrying an active span (e.g. logger.ErrorContext(ctx, ...)
// inside an otelhttp-instrumented request), per
// docs/tech-stack/observability-and-ops.md's "metrics, traces, and
// structured logs share trace/span IDs" requirement. A call site that
// still uses the context-free Info/Error methods behaves exactly as
// before, with no trace correlation — adopting *Context methods is an
// incremental, call-site-by-call-site improvement, not required by this
// change.
func NewLogger(serviceName string) *slog.Logger {
	return slog.New(newTraceHandler(slog.NewJSONHandler(os.Stdout, nil))).With("service", serviceName)
}

type traceHandler struct {
	next slog.Handler
}

func newTraceHandler(next slog.Handler) *traceHandler {
	return &traceHandler{next: next}
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, record slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{next: h.next.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{next: h.next.WithGroup(name)}
}
