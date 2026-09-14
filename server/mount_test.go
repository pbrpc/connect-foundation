package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2/connecthttp"
	"go.opentelemetry.io/otel"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"git.sonicoriginal.software/grpc-testing/mocks/listener"
)

// recordSpans installs an in-memory tracer provider as the global one for the
// life of the test, which is where otelhttp finds its tracer.
func recordSpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)

	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(t.Context())
	})

	return exporter
}

// echo sends text to procedure over the Connect protocol as JSON, through the
// mux and nothing else, and answers with the recorded response.
func echo(srv *Server, text string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, procedure, strings.NewReader(`"`+text+`"`))
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	srv.Mux.ServeHTTP(recorder, request)

	return recorder
}

func TestMount(t *testing.T) {
	t.Run("routes a procedure through the interceptors under a span", func(t *testing.T) {
		exporter := recordSpans(t)

		srv := New(slog.New(slog.DiscardHandler))
		srv.RPC.Register(echoMethod(nil))
		srv.Mount()

		recorder := echo(srv, "hello")

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %q, want 200", recorder.Code, recorder.Body.String())
		}
		if got := strings.TrimSpace(recorder.Body.String()); got != `"hello"` {
			t.Errorf("body = %q, want the echo", got)
		}

		spans := exporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("recorded %d spans, want 1", len(spans))
		}
		// otelhttp names the span by HTTP method and route, the HTTP
		// convention; the route is the procedure.
		if spans[0].Name != http.MethodPost+" "+procedure {
			t.Errorf("span name = %q, want the method and procedure", spans[0].Name)
		}
		// The span interceptor found the span otelhttp started, which is the
		// ordering the whole chain depends on.
		if !hasAttribute(spans[0].Attributes, semconv.RPCService(serviceName)) {
			t.Errorf("attributes = %v, want the RPC service", spans[0].Attributes)
		}
	})

	t.Run("applies the transport options", func(t *testing.T) {
		srv := New(slog.New(slog.DiscardHandler), WithTransportOptions(connecthttp.WithReadMaxBytes(1)))
		srv.RPC.Register(echoMethod(nil))
		srv.Mount()

		if recorder := echo(srv, "hello"); recorder.Code == http.StatusOK {
			t.Fatalf("status = %d, want the oversized message refused", recorder.Code)
		}
	})

	t.Run("applies the transport options from the environment", func(t *testing.T) {
		t.Setenv(EnvMaxRecvMsgSize, "1")
		t.Setenv(EnvMaxSendMsgSize, "1")

		srv := New(slog.New(slog.DiscardHandler))
		srv.RPC.Register(echoMethod(nil))
		srv.Mount()

		if recorder := echo(srv, "hello"); recorder.Code == http.StatusOK {
			t.Fatalf("status = %d, want the oversized message refused", recorder.Code)
		}
	})

	t.Run("wraps every route in the middleware, first outermost", func(t *testing.T) {
		var patterns []string

		record := func(name string) Middleware {
			return func(pattern string, next http.Handler) http.Handler {
				patterns = append(patterns, name+":"+pattern)

				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Add("X-Order", name)
					next.ServeHTTP(w, r)
				})
			}
		}

		srv := New(slog.New(slog.DiscardHandler), WithRouteMiddleware(record("outer"), record("inner")))
		srv.RPC.Register(echoMethod(nil))
		srv.Mount()

		for _, want := range []string{
			"outer:" + procedure, "inner:" + procedure,
			"outer:" + serviceRoot, "inner:" + serviceRoot,
		} {
			if !slices.Contains(patterns, want) {
				t.Errorf("patterns = %v, want %q", patterns, want)
			}
		}

		recorder := echo(srv, "hello")

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
		if got := recorder.Header().Values("X-Order"); !slices.Equal(got, []string{"outer", "inner"}) {
			t.Errorf("order = %v, want outer then inner", got)
		}
	})

	t.Run("mounts once", func(t *testing.T) {
		srv := New(slog.New(slog.DiscardHandler))
		srv.RPC.Register(echoMethod(nil))

		// A second registration of the same pattern panics the mux, so a
		// second Mount that did anything would not return.
		srv.Mount()
		srv.Mount()
	})
}

func TestServe(t *testing.T) {
	srv := New(slog.New(slog.DiscardHandler))
	srv.RPC.Register(echoMethod(nil))

	lis := listener.New()

	served := make(chan error, 1)
	go func() { served <- srv.Serve(lis) }()

	// Closing the listener is what ends Serve: Accept answers with the closed
	// error and Serve hands it back.
	_ = lis.Close()

	select {
	case err := <-served:
		if err == nil {
			t.Fatal("expected the closed listener to be reported")
		}
	case <-t.Context().Done():
		t.Fatal("Serve did not return after the listener closed")
	}

	// Serve mounted before listening.
	if recorder := echo(srv, "hello"); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want the procedure mounted", recorder.Code)
	}
}
