package server_test

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/connect/v2"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/pbrpc/connect-foundation/otel"
	"github.com/pbrpc/connect-foundation/server"
)

const cleanupTimeout = 5 * time.Second

// registerExampleService stands in for the caller's own service registration,
// which in practice is the generated RegisterXServiceHandler.
func registerExampleService(rpc *connect.Server) {
	rpc.Register(connect.Method{
		Spec: connect.Spec{
			StreamType: connect.StreamTypeUnary,
			Procedure:  "/example.ExampleService/Create",
		},
		Handler: func(_ context.Context, _ connect.Spec, stream connect.ServerStream) error {
			var request wrapperspb.StringValue
			if err := stream.Receive(&request); err != nil {
				return err
			}

			return stream.Send(&request)
		},
	})
}

// Example shows a service starting up and shutting down. It has no Output
// comment, so it is compiled but never run — its job is to keep this sequence
// type-checked against the real API.
func Example() {
	// Registered first so it runs last, after the teardown below has flushed.
	// Returning rather than calling os.Exit directly is what lets the defers run
	// at all.
	exitCode := 0
	defer func() { os.Exit(exitCode) }()

	// The process context. Everything the shutdown needs is built on this one,
	// which is why the signal cancels a child of it rather than this.
	ctx := context.Background()

	serveCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverName := server.Name("example")

	// TLS material comes from the environment when the listener terminates
	// TLS; with none set the listener is cleartext and WithTLS is a no-op.
	// Read before telemetry is up, so a failure is reported the way the
	// telemetry's own would be.
	tlsConfig, err := server.TLSConfig()
	if err != nil {
		slog.Default().Error("Failed to read TLS configuration", slog.Any("error", err))
		exitCode = 1
		return
	}

	log, flush, err := otel.Init(ctx, serverName, server.Version())
	if err != nil {
		slog.Default().Error("Failed to initialize telemetry", slog.Any("error", err))
		exitCode = 1
		return
	}

	// Init made log the process default, so a handler reaches it with
	// logger.FromContext(ctx) and its lines carry the request's span.
	srv := server.New(log, server.WithTLS(tlsConfig))

	// Deferred before anything else can fail, so every path out of here stops
	// the server and exports what it logged on the way.
	defer server.HandleGracefulShutdown(ctx, log, srv.HTTP, flush, cleanupTimeout)

	registerExampleService(srv.RPC)

	lis, err := server.Listen()
	if err != nil {
		log.Error("Failed to create listener", slog.Any("error", err))
		exitCode = 1
		return
	}

	log = log.With(slog.String("address", lis.Addr().String()))

	// Serve mounts what was registered and blocks. A deferred teardown cannot
	// run while it does, so it goes to a goroutine and the select below decides
	// when this returns.
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()

	log.Info("Connect server listening")

	select {
	case err := <-serveErr:
		if err != nil {
			log.Error("Failed to serve", slog.Any("error", err))
			exitCode = 1
		}
	case <-serveCtx.Done():
	}
}
