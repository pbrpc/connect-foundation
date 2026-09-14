package server

import (
	"fmt"
	"net"
)

// Listen creates a TCP listener on the configured server address.
// Callers pass the result to their service's Run, so choosing a listener stays
// the caller's decision rather than something Run does on their behalf.
func Listen() (net.Listener, error) {
	address := Address()

	lis, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", address, err)
	}

	return lis, nil
}
