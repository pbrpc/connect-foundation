package server

import (
	"context"
	"log/slog"
	"strings"

	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"git.sonicoriginal.software/logger"
)

// withTrace adds the trace identifiers to log when ctx carries a valid span
// context. The HTTP layer starts the server span before the connect handler
// runs, so a request that is being traced already has one here.
//
// otelslog puts these on the OTel record it exports, but the handlers writing
// to stdout never see them, so they have to be slog attributes to be printed.
func withTrace(ctx context.Context, log *slog.Logger) *slog.Logger {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return log
	}

	return log.With(
		slog.String("trace_id", spanContext.TraceID().String()),
		slog.String("span_id", spanContext.SpanID().String()),
	)
}

// makeLoggerInterceptor puts log into every request context. The transport
// builds each RPC's context from the incoming request rather than from the
// context the process started with, so a handler calling logger.FromContext
// would otherwise fall back to slog.Default() and its records would never
// reach the collector.
//
// One interceptor covers every stream type: a streaming handler reads its
// context from the same argument a unary one does.
func makeLoggerInterceptor(log *slog.Logger) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			return next(logger.ContextWithLogger(ctx, withTrace(ctx, log)), spec, stream)
		}
	}
}

// makeRecoveryInterceptor turns a panic in a handler into an Internal error,
// logging what was panicked with. A handler that panics would otherwise take
// the connection down with it.
func makeRecoveryInterceptor(log *slog.Logger) connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) (err error) {
			defer func() {
				if p := recover(); p != nil {
					log.Error("Recovered from panic", slog.Any("panic", p))

					err = connect.NewError(connect.CodeInternal, "internal server error")
				}
			}()

			return next(ctx, spec, stream)
		}
	}
}

// splitProcedure breaks "/package.Service/Method" into its service and method.
func splitProcedure(procedure string) (service, method string) {
	service, method, _ = strings.Cut(strings.TrimPrefix(procedure, "/"), "/")

	return service, method
}

// makeSpanInterceptor names the RPC on the span the HTTP layer started, and
// records the outcome on it. The HTTP span knows the route; this is what says
// which service and method that route is, and which Connect code the handler
// answered with.
func makeSpanInterceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			span := trace.SpanFromContext(ctx)

			service, method := splitProcedure(spec.Procedure)

			span.SetAttributes(
				semconv.RPCSystemConnectRPC,
				semconv.RPCService(service),
				semconv.RPCMethod(method),
			)

			err := next(ctx, spec, stream)
			if err != nil {
				code := connect.CodeOf(err)

				span.SetAttributes(semconv.RPCConnectRPCErrorCodeKey.String(code.String()))
				span.SetStatus(codes.Error, code.String())
			}

			return err
		}
	}
}
