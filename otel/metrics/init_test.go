package metrics

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func testResource() *resource.Resource {
	res, _ := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(semconv.ServiceName("test-service")),
	)
	return res
}

func TestInit(t *testing.T) {
	t.Run("otlp", func(t *testing.T) {
		t.Setenv(envVar, "otlp")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")

		shutdown, err := Init(t.Context(), testResource())
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if shutdown == nil {
			t.Fatal("expected non-nil shutdown")
		}

		// Nothing listens on the endpoint, so the flush is given a deadline it
		// will spend rather than a collector it will reach.
		timeoutCtx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()

		_ = shutdown(timeoutCtx)
	})

	for _, value := range []string{"none", ""} {
		t.Run("disabled by "+value, func(t *testing.T) {
			t.Setenv(envVar, value)

			shutdown, err := Init(t.Context(), testResource())
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if shutdown == nil {
				t.Fatal("expected non-nil shutdown")
			}
			if err := shutdown(t.Context()); err != nil {
				t.Fatalf("expected nil error from noop shutdown, got %v", err)
			}
		})
	}

	t.Run("invalid", func(t *testing.T) {
		t.Setenv(envVar, "invalid")

		shutdown, err := Init(t.Context(), testResource())
		if err == nil {
			t.Fatal("expected error for unsupported exporter")
		}
		if shutdown != nil {
			t.Fatal("expected nil shutdown on error")
		}
	})
}
