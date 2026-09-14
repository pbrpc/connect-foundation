package logging

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
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
	// Every stdout format is exercised against the OTLP provider, since the
	// handler chain is what differs between them.
	for _, format := range []string{"none", "json", "text", "flat", "structured", "", "JSON"} {
		t.Run("otlp with LOG_FORMAT "+format, func(t *testing.T) {
			t.Setenv(envVar, "otlp")
			t.Setenv("LOG_FORMAT", format)
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")

			logger, shutdown, err := Init(t.Context(), testResource(), "test-service")
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if logger == nil {
				t.Fatal("expected non-nil logger")
			}
			if shutdown == nil {
				t.Fatal("expected non-nil shutdown")
			}

			// Nothing listens on the endpoint, so the flush is given a deadline
			// it will spend rather than a collector it will reach.
			timeoutCtx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()

			_ = shutdown(timeoutCtx)
		})
	}

	for _, value := range []string{"none", ""} {
		t.Run("disabled by "+value, func(t *testing.T) {
			t.Setenv(envVar, value)
			t.Setenv("LOG_FORMAT", "structured")

			logger, shutdown, err := Init(t.Context(), testResource(), "test-service")
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if logger == nil {
				t.Fatal("expected non-nil logger")
			}
			if shutdown == nil {
				t.Fatal("expected non-nil shutdown")
			}
			if err := shutdown(t.Context()); err != nil {
				t.Fatalf("expected nil error from noop shutdown, got %v", err)
			}
		})
	}

	t.Run("routes SDK errors to stdout", func(t *testing.T) {
		t.Setenv(envVar, "none")
		t.Setenv("LOG_FORMAT", "json")

		if _, _, err := Init(t.Context(), testResource(), "test-service"); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// What the SDK does when an export fails. The handler Init installed
		// writes the record to stdout, so the only observable is that this
		// returns rather than recursing into the exporter.
		otel.Handle(errors.New("export failed"))
	})

	t.Run("invalid", func(t *testing.T) {
		t.Setenv(envVar, "invalid")

		_, shutdown, err := Init(t.Context(), testResource(), "test-service")
		if err == nil {
			t.Fatal("expected error for unsupported exporter")
		}
		if shutdown != nil {
			t.Fatal("expected nil shutdown on error")
		}
	})
}

func TestDefaultAttributeLevels(t *testing.T) {
	want := [][]string{
		{},
		{"source", "service", "address", "component", "trace_id"},
		{"span_id"},
		{"method"},
		{},
	}

	if !reflect.DeepEqual(DefaultAttributeLevels, want) {
		t.Fatalf("levels = %v, want %v", DefaultAttributeLevels, want)
	}
}
