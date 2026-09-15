package client

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"git.sonicoriginal.software/grpc-foundation/config"
)

const procedure = "/example.ExampleService/Echo"

// echoSpec describes procedure as a unary RPC over StringValue messages.
var echoSpec = connect.Spec{
	StreamType: connect.StreamTypeUnary,
	Procedure:  procedure,
}

// httpClientStub stands in for the HTTP client the transport drives. It
// records the request and answers by echoing the request body as a Connect
// unary response, so no socket is involved.
type httpClientStub struct {
	request *http.Request
}

func (s *httpClientStub) Do(request *http.Request) (*http.Response, error) {
	s.request = request

	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/proto"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    request,
	}, nil
}

func TestBaseURL(t *testing.T) {
	if got := BaseURL("service:50051"); got != "http://service:50051" {
		t.Errorf("BaseURL = %q, want http://service:50051", got)
	}
}

func TestNewTransport(t *testing.T) {
	t.Setenv(config.EnvKeepAliveTime, "4m")
	t.Setenv(config.EnvKeepAliveTimeout, "5s")

	transport := NewTransport()

	if !transport.Protocols.HTTP1() || !transport.Protocols.UnencryptedHTTP2() {
		t.Errorf("protocols = %v, want HTTP/1.1 and cleartext HTTP/2", transport.Protocols)
	}
	if transport.HTTP2.SendPingTimeout != 4*time.Minute {
		t.Errorf("send ping timeout = %v, want 4m", transport.HTTP2.SendPingTimeout)
	}
	if transport.HTTP2.PingTimeout != 5*time.Second {
		t.Errorf("ping timeout = %v, want 5s", transport.HTTP2.PingTimeout)
	}
}

func TestNewHTTPClient(t *testing.T) {
	t.Run("traces over the standard transport", func(t *testing.T) {
		httpClient := NewHTTPClient(nil)

		if _, ok := httpClient.Transport.(*otelhttp.Transport); !ok {
			t.Fatalf("transport = %T, want the tracing transport", httpClient.Transport)
		}
	})

	t.Run("traces over the caller's transport", func(t *testing.T) {
		base := newRoundTripperStub()

		httpClient := NewHTTPClient(base)

		if _, ok := httpClient.Transport.(*otelhttp.Transport); !ok {
			t.Fatalf("transport = %T, want the tracing transport", httpClient.Transport)
		}

		// The only way to see which transport is under the tracing one is to
		// send something through it. Nothing is dialed: base answers on its own.
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://service/", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := httpClient.Transport.RoundTrip(request); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if base.calls["service"] != 1 {
			t.Errorf("base transport calls = %d, want 1", base.calls["service"])
		}
	})
}

func TestNew(t *testing.T) {
	stub := &httpClientStub{}

	intercepted := false
	observe := func(next connect.ClientFunc) connect.ClientFunc {
		return func(ctx context.Context, spec connect.Spec) (connect.ClientStream, error) {
			intercepted = true

			return next(ctx, spec)
		}
	}

	rpc := New(stub, BaseURL("service:50051"), []connect.ClientInterceptor{observe})

	var response wrapperspb.StringValue

	if err := rpc.CallUnary(t.Context(), echoSpec, wrapperspb.String("hello"), &response); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response.GetValue() != "hello" {
		t.Errorf("response = %q, want hello", response.GetValue())
	}
	if !intercepted {
		t.Error("the interceptor did not run")
	}
	if got := stub.request.URL.String(); got != "http://service:50051"+procedure {
		t.Errorf("request URL = %q, want the procedure under the base URL", got)
	}
}
