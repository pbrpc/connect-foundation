package server

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"sync"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"git.sonicoriginal.software/logger"
	"github.com/pbrpc/connect-foundation/config"
)

// Middleware wraps the handler mounted for one route. pattern is the route as
// Mount registers it, "/package.Service/Method" for a procedure, so a
// middleware can act on one route and pass the rest through.
type Middleware func(pattern string, next http.Handler) http.Handler

// options collects what the caller adds to the standard server.
type options struct {
	interceptors []connect.ServerInterceptor
	transport    []connecthttp.Option
	middleware   []Middleware
	tls          *tls.Config
}

// Option customizes New.
type Option func(*options)

// WithInterceptors appends interceptors to the standard chain. They run after
// the standard ones, in the order given.
func WithInterceptors(interceptors ...connect.ServerInterceptor) Option {
	return func(o *options) {
		o.interceptors = append(o.interceptors, interceptors...)
	}
}

// WithTransportOptions appends options for connecthttp.Mount. They follow the
// standard ones, so a caller's message-size limit wins over the environment's.
func WithTransportOptions(transport ...connecthttp.Option) Option {
	return func(o *options) {
		o.transport = append(o.transport, transport...)
	}
}

// WithRouteMiddleware wraps every route on Mux, the procedures Mount
// registers and the plain routes a caller adds, each with the pattern it
// matched. The first middleware given is the outermost. This is where a
// server bounds the lifetime of one route's stream, by setting a write
// deadline on the response before handing it on; nothing in the HTTP layer
// names such a setting, so the foundation offers none.
func WithRouteMiddleware(middleware ...Middleware) Option {
	return func(o *options) {
		o.middleware = append(o.middleware, middleware...)
	}
}

// WithTLS terminates TLS on the listener with cfg, which supplies the
// certificate and whatever client authentication the caller wants. The server
// then accepts HTTP/1.1 and HTTP/2 over TLS, and Serve serves TLS. Without it
// the listener is cleartext.
func WithTLS(cfg *tls.Config) Option {
	return func(o *options) {
		o.tls = cfg
	}
}

// Server is the three things a Connect service serves through: the RPC
// dispatcher its handlers register on, the mux its HTTP routes go on, and the
// HTTP server that listens. Mount joins the first two; Serve does that and
// listens.
type Server struct {
	// RPC is the dispatcher. Generated Register functions add methods to it.
	RPC *connect.Server

	// Mux carries every route. Mount adds one per procedure; a caller adds
	// plain HTTP routes of its own beside them with Mux.Handle, and every
	// route is served the same way: under an HTTP span named by the pattern
	// it matched, through the route middleware.
	Mux *http.ServeMux

	// HTTP is the server that listens. Its handler serves Mux.
	HTTP *http.Server

	transport []connecthttp.Option
	mounted   sync.Once
}

// New creates a Connect server with production-grade defaults.
//
// Standard interceptors, outermost first: the RPC's service and method on the
// span the HTTP layer started, and panic recovery answering Internal, logged
// to log. Message-size limits come from GRPC_MAX_RECV_MSG_SIZE and
// GRPC_MAX_SEND_MSG_SIZE. The HTTP server accepts HTTP/1.1 and cleartext
// HTTP/2, or HTTP/1.1 and HTTP/2 over TLS when WithTLS is given; it closes a
// connection idle for GRPC_MAX_CONNECTION_IDLE, and pings one quiet for
// GRPC_KEEPALIVE_TIME, closing it when the ping goes unanswered for
// GRPC_KEEPALIVE_TIMEOUT.
//
// Every request runs under an HTTP span, and the process logger otel.Init
// built prints that span's identifiers on every line logged with the
// request's context, so a handler logs with logger.FromContext(ctx) and
// nothing has to be put into the context for it.
func New(log *slog.Logger, opts ...Option) *Server {
	if log == nil {
		log = logger.NewNullLogger()
	}

	var o options
	for _, opt := range opts {
		opt(&o)
	}

	interceptors := append([]connect.ServerInterceptor{
		makeSpanInterceptor(),
		makeRecoveryInterceptor(log),
	}, o.interceptors...)

	transport := append([]connecthttp.Option{
		connecthttp.WithReadMaxBytes(config.IntOrDefault(EnvMaxRecvMsgSize, DefaultMaxRecvMsgSize)),
		connecthttp.WithSendMaxBytes(config.IntOrDefault(EnvMaxSendMsgSize, DefaultMaxSendMsgSize)),
	}, o.transport...)

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	if o.tls != nil {
		protocols.SetHTTP2(true)
	} else {
		protocols.SetUnencryptedHTTP2(true)
	}

	mux := http.NewServeMux()

	return &Server{
		RPC: connect.NewServer(interceptors...),
		Mux: mux,
		HTTP: &http.Server{
			Handler:     handler(mux, o.middleware),
			IdleTimeout: config.DurationOrDefault(EnvMaxConnectionIdle, DefaultMaxConnectionIdle),
			Protocols:   protocols,
			TLSConfig:   o.tls,
			HTTP2: &http.HTTP2Config{
				SendPingTimeout: config.KeepAliveTime(),
				PingTimeout:     config.KeepAliveTimeout(),
			},
		},
		transport: transport,
	}
}
