package server

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2/connecthttp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/pbrpc/connect-testing/mocks/certificate"
	"github.com/pbrpc/connect-testing/mocks/listener"
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
// handler HTTP serves and nothing else, and answers with the recorded
// response.
func echo(srv *Server, text string) *httptest.ResponseRecorder {
	return serve(srv, httptest.NewRequest(http.MethodPost, procedure, strings.NewReader(`"`+text+`"`)))
}

// serve puts request through the handler HTTP serves and answers with the
// recorded response.
func serve(srv *Server, request *http.Request) *httptest.ResponseRecorder {
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	srv.HTTP.Handler.ServeHTTP(recorder, request)

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

	t.Run("wraps every route in the middleware, first outermost, with the pattern it matched", func(t *testing.T) {
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

		recorder := echo(srv, "hello")

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
		if got := recorder.Header().Values("X-Order"); !slices.Equal(got, []string{"outer", "inner"}) {
			t.Errorf("order = %v, want outer then inner", got)
		}
		// Wrapping runs innermost first, so the record reads inner then outer;
		// both saw the matched procedure.
		if want := []string{"inner:" + procedure, "outer:" + procedure}; !slices.Equal(patterns, want) {
			t.Errorf("patterns = %v, want %v", patterns, want)
		}
	})

	t.Run("serves a plain route on the mux the same way as a procedure", func(t *testing.T) {
		exporter := recordSpans(t)

		var patterns []string

		record := func(pattern string, next http.Handler) http.Handler {
			patterns = append(patterns, pattern)

			return next
		}

		var seen trace.SpanContext

		srv := New(slog.New(slog.DiscardHandler), WithRouteMiddleware(record))
		srv.Mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The span is in the route's context, which is where the process
			// logger reads it from on every line logged with that context.
			seen = trace.SpanContextFromContext(r.Context())

			w.WriteHeader(http.StatusNoContent)
		}))

		recorder := serve(srv, httptest.NewRequest(http.MethodGet, "/healthz", nil))

		if recorder.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want the route's own answer", recorder.Code)
		}
		if !slices.Equal(patterns, []string{"/healthz"}) {
			t.Errorf("patterns = %v, want the plain route wrapped with its pattern", patterns)
		}

		spans := exporter.GetSpans()
		if len(spans) != 1 || spans[0].Name != http.MethodGet+" /healthz" {
			t.Fatalf("spans = %v, want one named by method and route", spans)
		}
		if seen.TraceID() != spans[0].SpanContext.TraceID() {
			t.Errorf("handler saw trace %s, want the route's span %s", seen.TraceID(), spans[0].SpanContext.TraceID())
		}
	})

	t.Run("answers an unknown route the way the mux does", func(t *testing.T) {
		srv := New(slog.New(slog.DiscardHandler))

		if recorder := serve(srv, httptest.NewRequest(http.MethodGet, "/nowhere", nil)); recorder.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", recorder.Code)
		}
	})

	t.Run("mounts once", func(_ *testing.T) {
		srv := New(slog.New(slog.DiscardHandler))
		srv.RPC.Register(echoMethod(nil))

		// A second registration of the same pattern panics the mux, so a
		// second Mount that did anything would not return.
		srv.Mount()
		srv.Mount()
	})
}

// awaitServed waits for Serve to return after its listener was closed, and
// fails the test if it does not or reports nothing.
func awaitServed(t *testing.T, served <-chan error) {
	t.Helper()

	select {
	case err := <-served:
		if err == nil {
			t.Fatal("expected the closed listener to be reported")
		}
	case <-t.Context().Done():
		t.Fatal("Serve did not return after the listener closed")
	}
}

func TestServe(t *testing.T) {
	t.Run("serves cleartext", func(t *testing.T) {
		srv := New(slog.New(slog.DiscardHandler))
		srv.RPC.Register(echoMethod(nil))

		lis := listener.New()

		served := make(chan error, 1)
		go func() { served <- srv.Serve(lis) }()

		// Closing the listener is what ends Serve: Accept answers with the
		// closed error and Serve hands it back.
		_ = lis.Close()

		awaitServed(t, served)

		// Serve mounted before listening.
		if recorder := echo(srv, "hello"); recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want the procedure mounted", recorder.Code)
		}
	})

	t.Run("terminates TLS on the listener when configured", func(t *testing.T) {
		srv := New(slog.New(slog.DiscardHandler), WithTLS(&tls.Config{
			Certificates: []tls.Certificate{certificate.SelfSigned(t)},
		}))
		srv.RPC.Register(echoMethod(nil))

		lis, client := listener.NewPipe()

		served := make(chan error, 1)
		go func() { served <- srv.Serve(lis) }()

		// The handshake completes only against a TLS listener; a cleartext
		// one would read the ClientHello as an HTTP request and answer in
		// plain text, which the client cannot parse as a ServerHello.
		conn := tls.Client(client, &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h2"}}) //nolint:gosec // a self-signed test certificate

		if err := conn.HandshakeContext(t.Context()); err != nil {
			t.Fatalf("handshake failed: %v", err)
		}
		if got := conn.ConnectionState().NegotiatedProtocol; got != "h2" {
			t.Errorf("negotiated %q, want HTTP/2 over TLS", got)
		}

		_ = conn.Close()
		_ = lis.Close()

		awaitServed(t, served)
	})
}
