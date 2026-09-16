package logging

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// The attributes a record logged under a span carries, so a span in the
// trace store leads to its lines in the log store and back.
const (
	TraceIDKey = "trace_id"
	SpanIDKey  = "span_id"
)

// traceHandler adds the span a record was logged under to the record, then
// hands it on. The context every InfoContext call carries is where the span
// is, so a handler on any route logs correlated lines by construction, and
// the logger itself knows nothing of tracing.
type traceHandler struct {
	next slog.Handler
}

// withTrace wraps next so every record it receives carries its span.
func withTrace(next slog.Handler) slog.Handler {
	return traceHandler{next: next}
}

// Enabled asks next.
func (h traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle adds the trace and span identifiers from ctx to record, when ctx
// carries a valid span, and passes it to next.
func (h traceHandler) Handle(ctx context.Context, record slog.Record) error {
	if spanContext := trace.SpanContextFromContext(ctx); spanContext.IsValid() {
		record.AddAttrs(
			slog.String(TraceIDKey, spanContext.TraceID().String()),
			slog.String(SpanIDKey, spanContext.SpanID().String()),
		)
	}

	return h.next.Handle(ctx, record)
}

// WithAttrs answers with the same wrapping over next's.
func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup answers with the same wrapping over next's.
func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{next: h.next.WithGroup(name)}
}
