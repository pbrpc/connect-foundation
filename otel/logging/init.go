// Package logging initializes the OpenTelemetry LoggerProvider and builds
// the slog handler chain.
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"git.sonicoriginal.software/grpc-foundation/lifecycle"
	"git.sonicoriginal.software/logger/handlers/flat"
	"git.sonicoriginal.software/logger/handlers/json"
	"git.sonicoriginal.software/logger/handlers/structured"
	"git.sonicoriginal.software/logger/handlers/tee"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

const envVar = "OTEL_LOGS_EXPORTER"

// DefaultAttributeLevels defines the standard hierarchical logging structure
// for all services.
var DefaultAttributeLevels = [][]string{
	{}, // Level 0: message only
	{"source", "service", "address", "component", "trace_id"}, // Level 1: service-level context
	{"span_id"}, // Level 2: span context
	{"method"},  // Level 3: operation details
	{},          // Level 4: unknown attributes
}

// Init initializes logging based on OTEL_LOGS_EXPORTER and LOG_FORMAT.
//
// OTEL_LOGS_EXPORTER controls the OTel LoggerProvider:
//   - "otlp": OTLP/HTTP exporter to the collector's HTTP receiver
//   - "" or "none": no OTel log provider
//
// LOG_FORMAT controls the stdout bridge:
//   - "none": OTel only; stdout carries OTel SDK errors and nothing else
//   - "json": tee to stdout in JSON format
//   - "text" or "flat": tee to stdout in flat text format
//   - default (including "structured" or empty): tee to stdout in structured format
//
// Returns the logger, a ShutdownFunc for the LoggerProvider, and any error.
func Init(
	ctx context.Context, res *resource.Resource, serviceName string,
) (*slog.Logger, lifecycle.ShutdownFunc, error) {
	exporterType := os.Getenv(envVar)

	shutdown := func(context.Context) error { return nil }

	switch exporterType {
	case "otlp":
		// OTLP/HTTP exporters cannot fail at creation time: the first request
		// is what reaches the collector. Endpoint and TLS are configured via
		// OTEL_EXPORTER_OTLP_ENDPOINT.
		exporter, _ := otlploghttp.New(ctx)

		processor := sdklog.NewBatchProcessor(exporter)
		lp := sdklog.NewLoggerProvider(
			sdklog.WithProcessor(processor),
			sdklog.WithResource(res),
		)
		global.SetLoggerProvider(lp)

		shutdown = lp.Shutdown
	case "none", "":
	default:
		return nil, nil, fmt.Errorf("unsupported %s: %s (supported: otlp, none)", envVar, exporterType)
	}

	// Create handler AFTER SetLoggerProvider — otelslog captures the global
	// provider at creation time, not lazily per record.
	otelHandler := otelslog.NewHandler(serviceName)
	serviceAttr := slog.String("service", serviceName)

	format := strings.ToLower(os.Getenv("LOG_FORMAT"))

	var stdoutHandler slog.Handler
	switch format {
	case "json":
		stdoutHandler = json.NewHandler()
	case "text", "flat":
		stdoutHandler = flat.NewHandler()
	default: // "structured", "none" or empty
		stdoutHandler = structured.NewHandler(&structured.Options{
			AttributeLevels: DefaultAttributeLevels,
		})
	}

	// SDK errors go to stdout alone. An export failure reported through the
	// exporter that is failing cannot be delivered, and every attempt creates
	// another record that fails the same way.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.New(stdoutHandler).With(serviceAttr).Error("otel sdk", "error", err)
	}))

	handler := slog.Handler(otelHandler)
	if format != "none" {
		handler = tee.NewHandler(otelHandler, stdoutHandler)
	}

	return slog.New(handler).With(serviceAttr), shutdown, nil
}
