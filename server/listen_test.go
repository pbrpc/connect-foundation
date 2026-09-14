package server

import (
	"strings"
	"testing"
)

// TestListen is the one test in this module that touches a real network
// resource. Listen's whole job is to bind a socket, so there is no abstraction
// that verifies it without one — the stdlib tests net.Listen the same way.
// The bind is confined to this file: it takes an ephemeral loopback port,
// serves nothing, and is closed immediately. Nothing else in the codebase
// should need to do this.
func TestListen(t *testing.T) {
	t.Run("binds the configured address", func(t *testing.T) {
		t.Setenv(EnvServerAddress, "127.0.0.1:0")

		lis, err := Listen()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer lis.Close()

		if lis.Addr() == nil {
			t.Fatal("listener has no address")
		}
	})

	t.Run("reports the address it could not bind", func(t *testing.T) {
		address := "invalid:address:format"
		t.Setenv(EnvServerAddress, address)

		lis, err := Listen()
		if err == nil {
			t.Fatal("expected error")
		}
		if lis != nil {
			t.Fatalf("listener = %v, want nil", lis)
		}
		if !strings.Contains(err.Error(), address) {
			t.Errorf("error = %q, want it to name %q", err, address)
		}
	})
}
