package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// Any non-zero pair makes a span context valid, which is all the handler asks
// of one.
var (
	traceID = trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	spanID  = trace.SpanID{0x01, 0x02, 0x03, 0x04}
)

func TestWithTrace(t *testing.T) {
	t.Run("adds the span's identifiers to a record logged under it", func(t *testing.T) {
		var out bytes.Buffer
		log := slog.New(withTrace(slog.NewTextHandler(&out, nil)))

		spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID})

		log.InfoContext(trace.ContextWithSpanContext(t.Context(), spanContext), "message")

		line := out.String()
		if !strings.Contains(line, TraceIDKey+"="+traceID.String()) {
			t.Errorf("line = %q, want the trace ID", line)
		}
		if !strings.Contains(line, SpanIDKey+"="+spanID.String()) {
			t.Errorf("line = %q, want the span ID", line)
		}
	})

	t.Run("leaves a record logged under no span as it is", func(t *testing.T) {
		var out bytes.Buffer
		log := slog.New(withTrace(slog.NewTextHandler(&out, nil)))

		log.InfoContext(t.Context(), "message")

		if line := out.String(); strings.Contains(line, TraceIDKey) || strings.Contains(line, SpanIDKey) {
			t.Errorf("line = %q, want no trace fields", line)
		}
	})

	t.Run("keeps wrapping through With and WithGroup", func(t *testing.T) {
		var out bytes.Buffer
		log := slog.New(withTrace(slog.NewTextHandler(&out, nil))).With("component", "test").WithGroup("request")

		spanContext := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID})

		log.InfoContext(trace.ContextWithSpanContext(t.Context(), spanContext), "message", "id", 7)

		line := out.String()
		for _, want := range []string{"component=test", "request.id=7", traceID.String()} {
			if !strings.Contains(line, want) {
				t.Errorf("line = %q, want %s", line, want)
			}
		}
	})

	t.Run("answers Enabled from the handler it wraps", func(t *testing.T) {
		handler := withTrace(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn}))

		if handler.Enabled(t.Context(), slog.LevelInfo) {
			t.Error("expected Info to be disabled below the wrapped level")
		}
		if !handler.Enabled(t.Context(), slog.LevelError) {
			t.Error("expected Error to be enabled")
		}
	})
}
