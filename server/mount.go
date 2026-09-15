package server

import (
	"net"
	"net/http"
	"slices"

	"connectrpc.com/connect/v2/connecthttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// routes is the mux connecthttp.Mount registers on. Each route it receives is
// wrapped before reaching the real mux: the caller's middleware innermost, in
// order, and the HTTP span outermost, named by HTTP method and route ("POST
// /package.Service/Method"), so the span exists by the time anything else
// runs.
type routes struct {
	mux        connecthttp.ServeMux
	middleware []Middleware
}

// Handle wraps handler and registers it under pattern.
func (r *routes) Handle(pattern string, handler http.Handler) {
	for _, wrap := range slices.Backward(r.middleware) {
		handler = wrap(pattern, handler)
	}

	r.mux.Handle(pattern, otelhttp.NewHandler(handler, pattern))
}

// Mount registers a route on Mux for every procedure registered on RPC. It
// reads the registrations as they are when it is called, which is why it is
// its own step after they are made. Calling it again does nothing.
func (s *Server) Mount() {
	s.mounted.Do(func() {
		connecthttp.Mount(&routes{mux: s.Mux, middleware: s.middleware}, s.RPC, s.transport...)
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
