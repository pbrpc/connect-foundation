package otel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"git.sonicoriginal.software/grpc-foundation/lifecycle"
)

// disableExporters turns every exporter off, so a test that wants one on sets
// it after this.
func disableExporters(t *testing.T) {
	t.Helper()

	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
}

func TestInit(t *testing.T) {
	t.Run("with all exporters disabled", func(t *testing.T) {
		disableExporters(t)

		logger, shutdown, err := Init(t.Context(), "test-service", "1.2.3")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if logger == nil {
			t.Fatal("expected non-nil logger")
		}
		if shutdown == nil {
			t.Fatal("expected non-nil shutdown func")
		}
		if err := shutdown(t.Context()); err != nil {
			t.Errorf("shutdown error: %v", err)
		}
	})

	t.Run("with all otlp exporters", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
		t.Setenv("OTEL_METRICS_EXPORTER", "otlp")
		t.Setenv("OTEL_LOGS_EXPORTER", "otlp")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4318")

		logger, shutdown, err := Init(t.Context(), "test-service", "1.2.3")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if logger == nil {
			t.Fatal("expected non-nil logger")
		}
		if shutdown == nil {
			t.Fatal("expected non-nil shutdown func")
		}

		// Nothing listens on the endpoint, so the flush is given a deadline it
		// will spend rather than a collector it will reach.
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()

		_ = shutdown(ctx)
	})

	t.Run("with unsupported log exporter", func(t *testing.T) {
		disableExporters(t)
		t.Setenv("OTEL_LOGS_EXPORTER", "console")

		if _, _, err := Init(t.Context(), "test-service", "1.2.3"); err == nil {
			t.Fatal("expected error for unsupported log exporter")
		}
	})

	t.Run("with unsupported trace exporter", func(t *testing.T) {
		disableExporters(t)
		t.Setenv("OTEL_TRACES_EXPORTER", "invalid")

		if _, _, err := Init(t.Context(), "test-service", "1.2.3"); err == nil {
			t.Fatal("expected error for unsupported trace exporter")
		}
	})

	t.Run("with unsupported metric exporter", func(t *testing.T) {
		disableExporters(t)
		t.Setenv("OTEL_METRICS_EXPORTER", "invalid")

		if _, _, err := Init(t.Context(), "test-service", "1.2.3"); err == nil {
			t.Fatal("expected error for unsupported metric exporter")
		}
	})
}

// recordingShutdown appends name to order when run and answers with err.
func recordingShutdown(order *[]string, name string, err error) lifecycle.ShutdownFunc {
	return func(context.Context) error {
		*order = append(*order, name)

		return err
	}
}

func TestShutdownAll(t *testing.T) {
	t.Run("runs every shutdown in reverse order", func(t *testing.T) {
		var order []string

		shutdown := shutdownAll([]lifecycle.ShutdownFunc{
			recordingShutdown(&order, "logging", nil),
			recordingShutdown(&order, "tracing", nil),
			recordingShutdown(&order, "metrics", nil),
		})

		if err := shutdown(t.Context()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []string{"metrics", "tracing", "logging"}
		if !slices.Equal(order, want) {
			t.Errorf("order = %v, want %v", order, want)
		}
	})

	t.Run("keeps going past a failure and reports every one", func(t *testing.T) {
		var order []string

		shutdown := shutdownAll([]lifecycle.ShutdownFunc{
			recordingShutdown(&order, "logging", errors.New("logs unflushed")),
			recordingShutdown(&order, "tracing", nil),
			recordingShutdown(&order, "metrics", errors.New("metrics unflushed")),
		})

		err := shutdown(t.Context())
		if err == nil {
			t.Fatal("expected the failures to be reported")
		}
		if len(order) != 3 {
			t.Fatalf("ran %v, want all three", order)
		}
		for _, message := range []string{"logs unflushed", "metrics unflushed"} {
			if !strings.Contains(err.Error(), message) {
				t.Errorf("error = %q, want it to carry %q", err, message)
			}
		}
	})
}
