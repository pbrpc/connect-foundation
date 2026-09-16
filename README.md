# connect-foundation

A Connect service needs the same things settled before it answers its first
request: a server with connection limits and panic recovery, telemetry reporting
under one identity, a logger that reaches request handlers carrying the trace
they belong to, errors a caller can act on, and a shutdown that stops serving
and then exports what it recorded.

Each of those is a decision. Made once per service they drift, and two services
in one deployment end up disagreeing about what a timeout is or what an error
looks like. This library makes them once, for services built on
[connect-go](https://connectrpc.com) v2. Its handlers answer gRPC, gRPC-Web, and
Connect-protocol clients on one port.

## Installation

```bash
go get github.com/pbrpc/connect-foundation
```

## Usage

`server/example_test.go` holds the startup and shutdown sequence as a Go
`Example`. It has no `// Output:` comment, so `go test` compiles it and never
runs it, which keeps it type-checked against the real API. Read it there rather
than from a copy here.

## What's Included

- **`server/`** — `New` builds the RPC dispatcher, the mux, and the HTTP server;
  `Serve` mounts every registered procedure and listens;
  `HandleGracefulShutdown` stops serving and flushes telemetry against one
  deadline. Every route on the mux, procedure or plain `Mux.Handle`, is served
  under one HTTP span named by the pattern it matched, and
  `WithRouteMiddleware` wraps each with that pattern, which is where a server
  bounds one stream's lifetime by setting a write deadline on the response.
  `WithTLS` terminates TLS on the listener with the `*tls.Config`
  given, serving HTTP/1.1 and HTTP/2 over it; `TLSConfig` builds that config
  from the environment, and answers nil, which `WithTLS` reads as cleartext,
  when none is set.
- **`client/`** — `NewHTTPClient` and `New` build a `*connect.Client` with
  tracing and keepalive; `NewReadyTransport` holds requests to a host that
  could not be reached until its backoff schedule allows the next attempt,
  per host, the way a gRPC connection paced its reconnects.
- **`otel/`** — `Init` brings up logging, tracing, and metrics under one service
  identity, exporting over OTLP/HTTP. The logger it builds is the process
  default, and every line logged with a request's context carries that
  request's `trace_id` and `span_id`, on the exported record and on the stdout
  copy alike, so a span in the trace store leads to its lines in the log store
  and back.
- **`errors/`** — constructors for Connect errors carrying `google.rpc` details,
  each recording a span event.

Server configuration and keepalive settings are read by `config/`, under the
same variable names the gRPC foundation used, so a service moving from it keeps
its environment.

## Configuration

- `GRPC_SERVER_NAME`, `GRPC_SERVER_ADDRESS`, `GRPC_SERVER_VERSION` — the
  server's identity and listen address.
- `GRPC_MAX_CONNECTION_IDLE` — how long a connection sits idle before it is
  closed.
- `GRPC_KEEPALIVE_TIME`, `GRPC_KEEPALIVE_TIMEOUT` — how long a connection is
  quiet before it is pinged, and how long the ping goes unanswered before it is
  closed. Read by the server and the client.
- `GRPC_MAX_RECV_MSG_SIZE`, `GRPC_MAX_SEND_MSG_SIZE` — per-message limits.
- `TLS_CERT`, `TLS_KEY` — the listener's certificate and key as PEM, read by
  `server.TLSConfig`. Both unset is a cleartext listener. `TLS_CLIENT_CA` — a
  PEM CA; when set, every client must present a certificate it signed.
- `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER` — `otlp`
  or `none`.
- `OTEL_EXPORTER_OTLP_ENDPOINT` — the collector's OTLP/HTTP receiver.
- `LOG_FORMAT` — `structured`, `json`, `text`, or `none` for the stdout copy of
  the log.
