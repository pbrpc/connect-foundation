package server

import (
	"net"
	"net/http"
	"slices"

	"connectrpc.com/connect/v2/connecthttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// dispatcher is what the HTTP server serves: the mux, with every route it
// resolves wrapped in the caller's middleware, innermost last, before the
// handler runs. It is one handler over the whole mux, so a route registered
// any way at all is covered.
type dispatcher struct {
	mux        *http.ServeMux
	middleware []Middleware
}

// ServeHTTP asks the mux which route it will serve, wraps the mux in the
// middleware with that pattern, and serves through it, so the mux still does
// its own dispatch and the request carries the pattern and path values it
// sets. A request the mux has no route for gets the mux's own answer,
// unwrapped.
func (d *dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var handler http.Handler = d.mux

	if _, pattern := d.mux.Handler(r); pattern != "" {
		for _, wrap := range slices.Backward(d.middleware) {
			handler = wrap(pattern, handler)
		}
	}

	handler.ServeHTTP(w, r)
}

// handler answers with what HTTP serves: the dispatcher under one HTTP span
// per request, named by method and the route the mux matched once it has, so
// the span exists by the time any middleware or handler runs.
func handler(mux *http.ServeMux, middleware []Middleware) http.Handler {
	return otelhttp.NewHandler(&dispatcher{mux: mux, middleware: middleware}, "")
}

// Mount registers a route on Mux for every procedure registered on RPC. It
// reads the registrations as they are when it is called, which is why it is
// its own step after they are made. Calling it again does nothing.
func (s *Server) Mount() {
	s.mounted.Do(func() {
		connecthttp.Mount(s.Mux, s.RPC, s.transport...)
	})
}

// Serve mounts the procedures and serves on lis, terminating TLS there when
// the server was built WithTLS. It blocks the way http.Server.Serve does, and
// answers with what that answers with.
func (s *Server) Serve(lis net.Listener) error {
	s.Mount()

	if s.HTTP.TLSConfig != nil {
		// The certificate is in the config, so there are no files to name.
		return s.HTTP.ServeTLS(lis, "", "")
	}

	return s.HTTP.Serve(lis)
}
