package server

import (
	"context"
	"crypto/tls"
	"log/slog"
	"testing"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/pbrpc/connect-foundation/config"
)

func TestNew(t *testing.T) {
	t.Run("builds the three parts", func(t *testing.T) {
		srv := New(slog.New(slog.DiscardHandler))

		if srv.RPC == nil || srv.Mux == nil || srv.HTTP == nil || srv.HTTP.Handler == nil {
			t.Fatalf("server = %+v, want every part built", srv)
		}
		if !srv.HTTP.Protocols.HTTP1() || !srv.HTTP.Protocols.UnencryptedHTTP2() {
			t.Errorf("protocols = %v, want HTTP/1.1 and cleartext HTTP/2", srv.HTTP.Protocols)
		}
	})

	t.Run("terminates TLS when configured", func(t *testing.T) {
		cfg := &tls.Config{}

		srv := New(slog.New(slog.DiscardHandler), WithTLS(cfg))

		if srv.HTTP.TLSConfig != cfg {
			t.Error("the HTTP server does not carry the TLS config")
		}
		if !srv.HTTP.Protocols.HTTP1() || !srv.HTTP.Protocols.HTTP2() {
			t.Errorf("protocols = %v, want HTTP/1.1 and HTTP/2 over TLS", srv.HTTP.Protocols)
		}
		if srv.HTTP.Protocols.UnencryptedHTTP2() {
			t.Errorf("protocols = %v, want no cleartext HTTP/2 on a TLS listener", srv.HTTP.Protocols)
		}
	})

	t.Run("substitutes a logger when given none", func(t *testing.T) {
		srv := New(nil)

		// Recovery logs the panic. With no logger substituted, that log call
		// would itself panic on the nil logger, and this would not answer
		// Internal.
		srv.RPC.Register(echoMethod(func(context.Context) {
			panic("handler failure")
		}))

		_, err := callEcho(t, srv.RPC, "hello")
		if connect.CodeOf(err) != connect.CodeInternal {
			t.Fatalf("error = %v, want the recovered Internal", err)
		}
	})

	t.Run("reads connection settings from the environment", func(t *testing.T) {
		t.Setenv(EnvMaxConnectionIdle, "1m")
		t.Setenv(config.EnvKeepAliveTime, "4m")
		t.Setenv(config.EnvKeepAliveTimeout, "5s")

		srv := New(slog.New(slog.DiscardHandler))

		if srv.HTTP.IdleTimeout != time.Minute {
			t.Errorf("idle timeout = %v, want 1m", srv.HTTP.IdleTimeout)
		}
		if srv.HTTP.HTTP2.SendPingTimeout != 4*time.Minute {
			t.Errorf("send ping timeout = %v, want 4m", srv.HTTP.HTTP2.SendPingTimeout)
		}
		if srv.HTTP.HTTP2.PingTimeout != 5*time.Second {
			t.Errorf("ping timeout = %v, want 5s", srv.HTTP.HTTP2.PingTimeout)
		}
	})

	t.Run("runs the standard interceptors on every call", func(t *testing.T) {
		srv := New(slog.New(slog.DiscardHandler))

		srv.RPC.Register(echoMethod(func(context.Context) {
			panic("handler failure")
		}))

		_, err := callEcho(t, srv.RPC, "hello")
		if connect.CodeOf(err) != connect.CodeInternal {
			t.Fatalf("error = %v, want the recovered Internal", err)
		}
	})

	t.Run("runs the caller's interceptors after the standard ones", func(t *testing.T) {
		called := false
		observe := func(connect.ServerFunc) connect.ServerFunc {
			return func(context.Context, connect.Spec, connect.ServerStream) error {
				called = true

				// Inside the standard recovery, so this is answered as Internal
				// rather than taking the call down.
				panic("interceptor failure")
			}
		}

		srv := New(slog.New(slog.DiscardHandler), WithInterceptors(observe))

		srv.RPC.Register(echoMethod(nil))

		_, err := callEcho(t, srv.RPC, "hello")
		if connect.CodeOf(err) != connect.CodeInternal {
			t.Fatalf("error = %v, want the recovered Internal", err)
		}
		if !called {
			t.Error("the caller's interceptor did not run")
		}
	})
}
