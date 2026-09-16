package config

import (
	"testing"
	"time"
)

func TestKeepAliveTime(t *testing.T) {
	t.Run("returns default when environment variable not set", func(t *testing.T) {
		result := KeepAliveTime()
		if result != DefaultKeepAliveTime {
			t.Errorf("expected %v, got %v", DefaultKeepAliveTime, result)
		}
	})

	t.Run("returns environment variable when set", func(t *testing.T) {
		t.Setenv(EnvKeepAliveTime, "4m")
		result := KeepAliveTime()
		if result != 4*time.Minute {
			t.Errorf("expected %v, got %v", 4*time.Minute, result)
		}
	})
}

func TestKeepAliveTimeout(t *testing.T) {
	t.Run("returns default when environment variable not set", func(t *testing.T) {
		result := KeepAliveTimeout()
		if result != DefaultKeepAliveTimeout {
			t.Errorf("expected %v, got %v", DefaultKeepAliveTimeout, result)
		}
	})

	t.Run("returns environment variable when set", func(t *testing.T) {
		t.Setenv(EnvKeepAliveTimeout, "5s")
		result := KeepAliveTimeout()
		if result != 5*time.Second {
			t.Errorf("expected %v, got %v", 5*time.Second, result)
		}
	})
}
