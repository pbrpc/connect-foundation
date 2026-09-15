package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// procedure names the one method every test registers. Its service segment is
// what the span interceptor reports; its catch-all is what Mount adds beside it.
const (
	serviceName = "example.ExampleService"
	procedure   = "/" + serviceName + "/Echo"
	serviceRoot = "/" + serviceName + "/"
)

// echoSpec describes procedure as a unary RPC over StringValue messages. The
// schema is nil: nothing in the transports reads it, and a test has no
// descriptor to give.
var echoSpec = connect.Spec{
	StreamType: connect.StreamTypeUnary,
	Procedure:  procedure,
}

// echoMethod answers procedure by sending the request back, after running
// observe with the handler's context. observe is where a test looks at what
// the interceptors put there, or panics to see what happens.
func echoMethod(observe func(ctx context.Context)) connect.Method {
	return connect.Method{
		Spec: echoSpec,
		Handler: func(ctx context.Context, _ connect.Spec, stream connect.ServerStream) error {
			var request wrapperspb.StringValue
			if err := stream.Receive(&request); err != nil {
				return err
			}

			if observe != nil {
				observe(ctx)
			}

			return stream.Send(&request)
		},
	}
}

// callEcho drives procedure on rpc in-process, with no listener, and answers
// with what came back.
func callEcho(t *testing.T, rpc *connect.Server, text string) (string, error) {
	t.Helper()

	client := connect.NewClient(connectinprocess.New(rpc))

	var response wrapperspb.StringValue

	err := client.CallUnary(t.Context(), echoSpec, wrapperspb.String(text), &response)

	return response.GetValue(), err
}

// serverStreamStub is the stream an interceptor hands on when a test invokes
// it directly rather than through a transport. Nothing is sent or received on
// it; the interceptors under test never touch the stream.
type serverStreamStub struct {
	connect.ServerStream
}

// pipeListener is a net.Listener with no port. Accept hands out the server
// end of one in-memory pipe, then waits for Close; the client end is the
// test's to speak on.
type pipeListener struct {
	conn   net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() (*pipeListener, net.Conn) {
	server, client := net.Pipe()

	return &pipeListener{conn: server, closed: make(chan struct{})}, client
}

func (l *pipeListener) Accept() (net.Conn, error) {
	if conn := l.conn; conn != nil {
		l.conn = nil

		return conn, nil
	}

	<-l.closed

	return nil, net.ErrClosed
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })

	return nil
}

func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }

// selfSigned answers with a certificate good for a TLS handshake a client
// does not verify.
func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// selfSignedPEM answers with a self-signed certificate and its key as PEM,
// the form the environment carries them in.
func selfSignedPEM(t *testing.T) (certPEM, keyPEM string) {
	t.Helper()

	cert := selfSigned(t)

	key, err := x509.MarshalECPrivateKey(cert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: key}))
}
