// Package server provides Connect server creation and lifecycle management.
package server

import (
	"time"

	"github.com/pbrpc/connect-foundation/config"
)

// Default server settings.
const (
	DefaultAddress = ":50051"
	DefaultVersion = "dev"

	DefaultMaxConnectionIdle = 5 * time.Minute
	DefaultMaxRecvMsgSize    = 4 * 1024 * 1024
	DefaultMaxSendMsgSize    = 4 * 1024 * 1024
)

// Environment variable names for runtime configuration. They are the ones the
// gRPC foundation reads, so a service moving between the two keeps its
// deployment.
const (
	EnvServerName        = "GRPC_SERVER_NAME"
	EnvServerAddress     = "GRPC_SERVER_ADDRESS"
	EnvServerVersion     = "GRPC_SERVER_VERSION"
	EnvMaxConnectionIdle = "GRPC_MAX_CONNECTION_IDLE"
	EnvMaxRecvMsgSize    = "GRPC_MAX_RECV_MSG_SIZE"
	EnvMaxSendMsgSize    = "GRPC_MAX_SEND_MSG_SIZE"
)

// Address returns the server address from environment or the default value.
func Address() string {
	return config.StringOrDefault(EnvServerAddress, DefaultAddress)
}

// Version returns the server version from environment or the default value.
func Version() string {
	return config.StringOrDefault(EnvServerVersion, DefaultVersion)
}

// Name returns the server name from environment or the caller's fallback.
// There is no universal default: the fallback is the server's own identity,
// and the environment overrides it per deployment.
func Name(fallback string) string {
	return config.StringOrDefault(EnvServerName, fallback)
}
