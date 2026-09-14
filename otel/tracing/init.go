// Package tracing initializes the OpenTelemetry TracerProvider.
package tracing

import (
	"context"
	"fmt"
	"os"

	"git.sonicoriginal.software/grpc-foundation/lifecycle"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const envVar = "OTEL_TRACES_EXPORTER"

// Init initializes the OTel TracerProvider based on OTEL_TRACES_EXPORTER.
//
//   - "otlp": OTLP/HTTP exporter to the collector's HTTP receiver
//   - "" or "none": no tracing (returns noop shutdown)
//
// Returns a ShutdownFunc to flush and close the provider.
func Init(
	ctx context.Context, res *resource.Resource,
) (lifecycle.ShutdownFunc, error) {
	exporterType := os.Getenv(envVar)

	if exporterType == "" || exporterType == "none" {
		noop := func(context.Context) error { return nil }
		return noop, nil
	}

	if exporterType != "otlp" {
		return nil, fmt.Errorf("unsupported %s: %s (supported: otlp, none)", envVar, exporterType)
	}

	// OTLP/HTTP exporters cannot fail at creation time: the first request is
	// what reaches the collector, and a failure is reported through the
	// error handler then. Endpoint and TLS are configured via
	// OTEL_EXPORTER_OTLP_ENDPOINT.
	exporter, _ := otlptracehttp.New(ctx)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	propagator := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(propagator)

	return tp.Shutdown, nil
}
