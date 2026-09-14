package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"git.sonicoriginal.software/grpc-testing/mocks/tracer"
	"git.sonicoriginal.software/logger"
)

// Any non-zero pair makes a span context valid, which is all withTrace asks of
// one.
var (
	traceID = trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	spanID  = trace.SpanID{0x01, 0x02, 0x03, 0x04}
)

// handlerStub is the ServerFunc an interceptor wraps in these tests. It
// records the context it was given and answers with err, or panics with
// panicValue when that is set.
type handlerStub struct {
	ctx        context.Context
	err        error
	panicValue any
}

func (h *handlerStub) serve(ctx context.Context, _ connect.Spec, _ connect.ServerStream) error {
	h.ctx = ctx

	if h.panicValue != nil {
		panic(h.panicValue)
	}

	return h.err
}

func TestWithTrace(t *testing.T) {
	t.Run("adds the trace identifiers when the context carries a span", func(t *testing.T) {
		var out bytes.Buffer
		log := slog.New(slog.NewTextHandler(&out, nil))

		spanContext := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: traceID,
			SpanID:  spanID,
		})

		withTrace(trace.ContextWithSpanContext(t.Context(), spanContext), log).Info("message")

		if !strings.Contains(out.String(), traceID.String()) {
			t.Errorf("output = %q, want it to carry the trace ID", out.String())
		}
		if !strings.Contains(out.String(), spanID.String()) {
			t.Errorf("output = %q, want it to carry the span ID", out.String())
		}
	})

	t.Run("returns the logger unchanged when the context carries no span", func(t *testing.T) {
		log := slog.New(slog.NewTextHandler(io.Discard, nil))

		if withTrace(t.Context(), log) != log {
			t.Error("expected the logger it was given")
		}
	})
}

func TestMakeLoggerInterceptor(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := &handlerStub{}

	serve := makeLoggerInterceptor(log)(handler.serve)

	if err := serve(t.Context(), echoSpec, serverStreamStub{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logger.FromContext(handler.ctx) != log {
		t.Error("the handler did not receive the logger")
	}
}

func TestMakeRecoveryInterceptor(t *testing.T) {
	t.Run("passes a handler's answer through", func(t *testing.T) {
		want := errors.New("handler failed")
		handler := &handlerStub{err: want}

		serve := makeRecoveryInterceptor(slog.New(slog.DiscardHandler))(handler.serve)

		if err := serve(t.Context(), echoSpec, serverStreamStub{}); !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
		}
	})

	t.Run("answers Internal and logs when the handler panics", func(t *testing.T) {
		var out bytes.Buffer
		log := slog.New(slog.NewTextHandler(&out, nil))
		handler := &handlerStub{panicValue: "test panic value"}

		serve := makeRecoveryInterceptor(log)(handler.serve)

		err := serve(t.Context(), echoSpec, serverStreamStub{})
		if connect.CodeOf(err) != connect.CodeInternal {
			t.Fatalf("code = %v, want %v", connect.CodeOf(err), connect.CodeInternal)
		}

		var connectErr *connect.Error
		if !errors.As(err, &connectErr) || connectErr.Message() != "internal server error" {
			t.Errorf("error = %v, want the fixed internal message", err)
		}
		if !strings.Contains(out.String(), "test panic value") {
			t.Errorf("log = %q, want it to carry the panic value", out.String())
		}
	})
}

func TestSplitProcedure(t *testing.T) {
	cases := []struct {
		procedure, service, method string
	}{
		{"/example.ExampleService/Echo", "example.ExampleService", "Echo"},
		{"example.ExampleService/Echo", "example.ExampleService", "Echo"},
		{"/example.ExampleService/", "example.ExampleService", ""},
		{"/example.ExampleService", "example.ExampleService", ""},
		{"", "", ""},
	}

	for _, c := range cases {
		t.Run(c.procedure, func(t *testing.T) {
			service, method := splitProcedure(c.procedure)

			if service != c.service || method != c.method {
				t.Errorf("got (%q, %q), want (%q, %q)", service, method, c.service, c.method)
			}
		})
	}
}

// spanAttributes answers with the attributes of the one span the mock recorded.
func spanAttributes(t *testing.T, tt *tracer.Mock) []attribute.KeyValue {
	t.Helper()

	tt.EndSpan()

	spans := tt.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(spans))
	}

	return spans[0].Attributes
}

// hasAttribute reports whether attrs carries want.
func hasAttribute(attrs []attribute.KeyValue, want attribute.KeyValue) bool {
	for _, attr := range attrs {
		if attr.Key == want.Key && attr.Value == want.Value {
			return true
		}
	}

	return false
}

func TestMakeSpanInterceptor(t *testing.T) {
	t.Run("names the RPC on the span", func(t *testing.T) {
		tt, ctx := tracer.New(t)
		defer tt.Shutdown(t)

		handler := &handlerStub{}

		if err := makeSpanInterceptor()(handler.serve)(ctx, echoSpec, serverStreamStub{}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		attrs := spanAttributes(t, tt)

		for _, want := range []attribute.KeyValue{
			semconv.RPCSystemConnectRPC,
			semconv.RPCService(serviceName),
			semconv.RPCMethod("Echo"),
		} {
			if !hasAttribute(attrs, want) {
				t.Errorf("attributes = %v, want %v", attrs, want)
			}
		}
		if hasAttribute(attrs, semconv.RPCConnectRPCErrorCodeKey.String("internal")) {
			t.Error("a successful call must carry no error code")
		}
	})

	t.Run("records the error code and status when the handler fails", func(t *testing.T) {
		tt, ctx := tracer.New(t)
		defer tt.Shutdown(t)

		want := connect.NewError(connect.CodeNotFound, "missing")
		handler := &handlerStub{err: want}

		err := makeSpanInterceptor()(handler.serve)(ctx, echoSpec, serverStreamStub{})
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
		}

		tt.EndSpan()

		span := tt.GetSpans()[0]

		if !hasAttribute(span.Attributes, semconv.RPCConnectRPCErrorCodeKey.String("not_found")) {
			t.Errorf("attributes = %v, want the not_found code", span.Attributes)
		}
		if span.Status.Code != codes.Error {
			t.Errorf("status = %v, want %v", span.Status.Code, codes.Error)
		}
	})
}
