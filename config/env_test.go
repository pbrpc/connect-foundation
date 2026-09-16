package config

import (
	"testing"
	"time"
)

func TestStringOrDefault(t *testing.T) {
	t.Run("returns environment variable when set", func(t *testing.T) {
		t.Setenv("TEST_VAR", "custom-value")
		result := StringOrDefault("TEST_VAR", "default-value")
		if result != "custom-value" {
			t.Errorf("expected %q, got %q", "custom-value", result)
		}
	})

	t.Run("returns default when environment variable not set", func(t *testing.T) {
		result := StringOrDefault("NONEXISTENT_VAR", "default-value")
		if result != "default-value" {
			t.Errorf("expected %q, got %q", "default-value", result)
		}
	})

	t.Run("returns default when environment variable is empty", func(t *testing.T) {
		t.Setenv("EMPTY_VAR", "")
		result := StringOrDefault("EMPTY_VAR", "default-value")
		if result != "default-value" {
			t.Errorf("expected %q, got %q", "default-value", result)
		}
	})
}

func TestDurationOrDefault(t *testing.T) {
	t.Run("returns parsed duration when valid", func(t *testing.T) {
		t.Setenv("DURATION_VAR", "10m")
		result := DurationOrDefault("DURATION_VAR", 5*time.Minute)
		if result != 10*time.Minute {
			t.Errorf("expected %v, got %v", 10*time.Minute, result)
		}
	})

	t.Run("returns default when not set", func(t *testing.T) {
		result := DurationOrDefault("NONEXISTENT_DURATION", 5*time.Minute)
		if result != 5*time.Minute {
			t.Errorf("expected %v, got %v", 5*time.Minute, result)
		}
	})

	t.Run("returns default when invalid format", func(t *testing.T) {
		t.Setenv("INVALID_DURATION", "not-a-duration")
		result := DurationOrDefault("INVALID_DURATION", 5*time.Minute)
		if result != 5*time.Minute {
			t.Errorf("expected %v, got %v", 5*time.Minute, result)
		}
	})

	t.Run("handles various duration formats", func(t *testing.T) {
		t.Setenv("SECONDS_VAR", "30s")
		result := DurationOrDefault("SECONDS_VAR", time.Minute)
		if result != 30*time.Second {
			t.Errorf("expected %v, got %v", 30*time.Second, result)
		}
	})
}

func TestIntOrDefault(t *testing.T) {
	t.Run("returns parsed int when valid", func(t *testing.T) {
		t.Setenv("INT_VAR", "42")
		result := IntOrDefault("INT_VAR", 10)
		if result != 42 {
			t.Errorf("expected %d, got %d", 42, result)
		}
	})

	t.Run("returns default when not set", func(t *testing.T) {
		result := IntOrDefault("NONEXISTENT_INT", 10)
		if result != 10 {
			t.Errorf("expected %d, got %d", 10, result)
		}
	})

	t.Run("returns default when invalid format", func(t *testing.T) {
		t.Setenv("INVALID_INT", "not-an-int")
		result := IntOrDefault("INVALID_INT", 10)
		if result != 10 {
			t.Errorf("expected %d, got %d", 10, result)
		}
	})

	t.Run("handles negative numbers", func(t *testing.T) {
		t.Setenv("NEGATIVE_INT", "-5")
		result := IntOrDefault("NEGATIVE_INT", 10)
		if result != -5 {
			t.Errorf("expected %d, got %d", -5, result)
		}
	})
}
