package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/pbrpc/connect-foundation/lifecycle"
)

// Shutdowner stops a server once its in-flight requests finish, or once ctx
// ends, whichever comes first. *http.Server satisfies it.
type Shutdowner interface {
	Shutdown(ctx context.Context) error
}

// HandleGracefulShutdown stops the HTTP server and then flushes the telemetry
// providers, both against one deadline built from cleanupTimeout. Whatever
// stopping the server spends, the flush does not get.
//
// The flush runs second because the requests that drain during Shutdown
// produce the spans and logs it exports.
//
// Callers defer it, so it runs on every path out of main: a listener that fails
// to bind flushes the error explaining that failure the same way a signal
// flushes the requests that drained.
//
// ctx is the process context. Passing the context that was cancelled to trigger
// the shutdown leaves nothing for the deadline to be built on and the flush
// exports nothing.
func HandleGracefulShutdown(
	ctx context.Context,
	log *slog.Logger,
	srv Shutdowner,
	flush lifecycle.ShutdownFunc,
	cleanupTimeout time.Duration,
) {
	shutdownLog := log.With(slog.String("component", "shutdown-handler"))
	shutdownLog.Info("Initiating graceful shutdown")

	shutdownCtx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()

	shutdownLog.Info("Stopping server")

	// Shutdown returns the context's error when in-flight requests outlast the
	// deadline; a held stream never goes idle, so that is the expected way out
	// for a server holding one.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		shutdownLog.Warn("Timed out waiting for in-flight requests",
			slog.Duration("timeout", cleanupTimeout), slog.Any("error", err))
	} else {
		shutdownLog.Info("Server stopped")
	}

	shutdownLog.Info("Flushing telemetry")

	if err := flush(shutdownCtx); err != nil {
		shutdownLog.Warn("Telemetry flush incomplete", slog.Any("error", err))
		return
	}

	shutdownLog.Info("Telemetry flushed")
}
