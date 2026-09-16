//revive:disable:package-comments
package config

import "time"

// Keepalive settings. Both the server and the client read them: the server to
// notice a client that went silent, the client to notice a server that did.
const (
	EnvKeepAliveTime    = "GRPC_KEEPALIVE_TIME"
	EnvKeepAliveTimeout = "GRPC_KEEPALIVE_TIMEOUT"

	DefaultKeepAliveTime    = 2 * time.Minute
	DefaultKeepAliveTimeout = 20 * time.Second
)

// KeepAliveTime answers with how long a connection is quiet before a ping is
// sent on it.
func KeepAliveTime() time.Duration {
	return DurationOrDefault(EnvKeepAliveTime, DefaultKeepAliveTime)
}

// KeepAliveTimeout answers with how long a ping goes unanswered before the
// connection is closed.
func KeepAliveTimeout() time.Duration {
	return DurationOrDefault(EnvKeepAliveTimeout, DefaultKeepAliveTimeout)
}
