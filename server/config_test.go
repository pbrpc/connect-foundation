package server

import (
	"testing"
)

func TestAddress(t *testing.T) {
	t.Run("returns default when environment variable not set", func(t *testing.T) {
		result := Address()
		if result != DefaultAddress {
			t.Errorf("expected %q, got %q", DefaultAddress, result)
		}
	})

	t.Run("returns environment variable when set", func(t *testing.T) {
		t.Setenv(EnvServerAddress, ":9090")
		result := Address()
		if result != ":9090" {
			t.Errorf("expected %q, got %q", ":9090", result)
		}
	})
}

func TestName(t *testing.T) {
	fallback := "grpcd"

	t.Run("returns the fallback when environment variable not set", func(t *testing.T) {
		result := Name(fallback)
		if result != fallback {
			t.Errorf("expected %q, got %q", fallback, result)
		}
	})

	t.Run("returns environment variable when set", func(t *testing.T) {
		t.Setenv(EnvServerName, "grpcd-canary")
		result := Name(fallback)
		if result != "grpcd-canary" {
			t.Errorf("expected %q, got %q", "grpcd-canary", result)
		}
	})
}

func TestVersion(t *testing.T) {
	t.Run("returns default when environment variable not set", func(t *testing.T) {
		result := Version()
		if result != DefaultVersion {
			t.Errorf("expected %q, got %q", DefaultVersion, result)
		}
	})

	t.Run("returns environment variable when set", func(t *testing.T) {
		t.Setenv(EnvServerVersion, "1.2.3")
		result := Version()
		if result != "1.2.3" {
			t.Errorf("expected %q, got %q", "1.2.3", result)
		}
	})
}
