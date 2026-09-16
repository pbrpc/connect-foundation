// Package metrics initializes the OpenTelemetry MeterProvider.
package metrics

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/pbrpc/connect-foundation/lifecycle"
)

const envVar = "OTEL_METRICS_EXPORTER"

// Init initializes the OTel MeterProvider based on OTEL_METRICS_EXPORTER.
//
//   - "otlp": OTLP/HTTP exporter to the collector's HTTP receiver
//   - "" or "none": no metrics (returns noop shutdown)
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
	// what reaches the collector. Endpoint and TLS are configured via
	// OTEL_EXPORTER_OTLP_ENDPOINT.
	exporter, _ := otlpmetrichttp.New(ctx)

	reader := sdkmetric.NewPeriodicReader(exporter)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}
