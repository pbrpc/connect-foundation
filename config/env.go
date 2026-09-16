//revive:disable:package-comments
package config

import (
	"os"
	"strconv"
	"time"
)

// StringOrDefault answers with the environment variable named key, or fallback
// when it is unset or empty.
func StringOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}

// DurationOrDefault parses the environment variable named key as a duration,
// answering with fallback when it is unset or does not parse.
func DurationOrDefault(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}

	return fallback
}

// IntOrDefault parses the environment variable named key as an int, answering
// with fallback when it is unset or does not parse.
func IntOrDefault(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}

	return fallback
}
