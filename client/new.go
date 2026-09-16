//revive:disable:package-comments
package client

import (
	"net/http"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"git.sonicoriginal.software/grpc-foundation/config"
)

// BaseURL answers with the URL a Connect client is built against for a
// host:port address on the internal mesh: cleartext HTTP, the same posture as
// a gRPC client's insecure credentials.
func BaseURL(address string) string {
	return "http://" + address
}

// NewTransport is the standard HTTP transport: cleartext HTTP/2 to the
// internal mesh, with keepalive pings read from GRPC_KEEPALIVE_TIME and
// GRPC_KEEPALIVE_TIMEOUT, sent whether or not an RPC is active so a peer that
// went silent is noticed on a held connection and not only on the next call.
// It is what NewHTTPClient and NewReadyTransport use when given no base, and
// what a caller wraps when it puts its own RoundTripper under them.
//
// HTTP/2 is the only protocol enabled: net/http speaks unencrypted HTTP/2 to
// an http:// URL only when HTTP/1 is not also enabled, and gRPC and the
// streaming RPCs need HTTP/2. Every server on the mesh accepts it.
func NewTransport() *http.Transport {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)

	return &http.Transport{
		Protocols: protocols,
		HTTP2: &http.HTTP2Config{
			SendPingTimeout: config.KeepAliveTime(),
			PingTimeout:     config.KeepAliveTimeout(),
		},
	}
}

// NewHTTPClient creates an HTTP client with standardized options: OpenTelemetry
// trace propagation over base, which is the standard transport when nil. A
// caller supplies base to put its own RoundTripper under the tracing, for
// instance one that chooses the host per request.
func NewHTTPClient(base http.RoundTripper) *http.Client {
	if base == nil {
		base = NewTransport()
	}

	return &http.Client{Transport: otelhttp.NewTransport(base)}
}

// New creates a Connect client that dispatches over httpClient against
// baseURL, with the given interceptors and transport options. The generated
// service clients are built from it:
//
//	rpc := client.New(client.NewHTTPClient(nil), client.BaseURL("service:50051"), nil)
//	svc := examplev1connect.NewExampleServiceClient(rpc)
//
// For a peer that speaks only gRPC, pass connecthttp.WithGRPC().
func New(
	httpClient connecthttp.HTTPClient,
	baseURL string,
	interceptors []connect.ClientInterceptor,
	opts ...connecthttp.Option,
) *connect.Client {
	return connect.NewClient(connecthttp.NewTransport(httpClient, baseURL, opts...), interceptors...)
}
