package server

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connectinprocess"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// procedure names the one method every test registers. Its service segment is
// what the span interceptor reports.
const (
	serviceName = "example.ExampleService"
	procedure   = "/" + serviceName + "/Echo"
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
