// Package otel provides OpenTelemetry initialization for tracing, metrics, and logging.
package otel

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"git.sonicoriginal.software/grpc-foundation/lifecycle"

	"git.sonicoriginal.software/connect-foundation/otel/logging"
	"git.sonicoriginal.software/connect-foundation/otel/metrics"
	"git.sonicoriginal.software/connect-foundation/otel/tracing"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Init initializes all OpenTelemetry providers: logging first (so tracing and
// metrics initialization can log), then tracing, then metrics.
//
// Name and version are supplied by the caller, which reads them from the
// server configuration every other part of the process reads. They win over
// OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES, so a server's identity has
// one source rather than one per telemetry vendor.
//
// Every exporter speaks OTLP over HTTP, so OTEL_EXPORTER_OTLP_ENDPOINT names
// the collector's HTTP receiver.
//
// Returns the logger and a ShutdownFunc that tears down all providers in
// reverse order (metrics, tracing, logging).
func Init(
	ctx context.Context, serviceName, version string,
) (*slog.Logger, lifecycle.ShutdownFunc, error) {
	// resource.Merge only fails when resources have incompatible schema URLs.
	// Since we use NewSchemaless (no schema URL), this cannot fail.
	res, _ := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
		),
	)

	var shutdowns []lifecycle.ShutdownFunc

	// Logging first so tracing and metrics init can log.
	log, logShutdown, err := logging.Init(ctx, res, serviceName)
	if err != nil {
		return nil, nil, fmt.Errorf("logging init: %w", err)
	}
	shutdowns = append(shutdowns, logShutdown)

	tracerShutdown, err := tracing.Init(ctx, res)
	if err != nil {
		return nil, nil, fmt.Errorf("tracing init: %w", err)
	}
	shutdowns = append(shutdowns, tracerShutdown)

	meterShutdown, err := metrics.Init(ctx, res)
	if err != nil {
		return nil, nil, fmt.Errorf("metrics init: %w", err)
	}
	shutdowns = append(shutdowns, meterShutdown)

	return log, shutdownAll(shutdowns), nil
}

// shutdownAll answers with one ShutdownFunc that runs every one of shutdowns
// in reverse, so providers stop in the opposite order they started: metrics,
// tracing, logging last. Every one runs whether or not an earlier one failed,
// and the failures are reported together.
func shutdownAll(shutdowns []lifecycle.ShutdownFunc) lifecycle.ShutdownFunc {
	return func(ctx context.Context) error {
		var errs []error

		for _, fn := range slices.Backward(shutdowns) {
			if err := fn(ctx); err != nil {
				errs = append(errs, err)
			}
		}

		if len(errs) > 0 {
			return fmt.Errorf("otel shutdown errors: %v", errs)
		}

		return nil
	}
}
