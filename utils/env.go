// Package utils provides small shared helpers (env access, logging setup).
// It is helpers only — per-binary config binding lives in each cmd.
package utils

import (
	"fmt"
	"os"
	"strconv"

	"github.com/rs/zerolog"
)

// GetEnv returns the environment variable value or a default.
func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvRequired returns the environment variable value or fatals with logger.
func GetEnvRequired(key string, logger zerolog.Logger) string {
	value := os.Getenv(key)
	if value == "" {
		logger.Fatal().Str("env_var", key).Msg("required environment variable is not set")
	}
	return value
}

// GetEnvInt returns the environment variable parsed as an int, or the
// default when unset. A malformed value is an error the caller must
// answer: a silently defaulted limit would weaken a policy bound.
func GetEnvInt(key string, defaultValue int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("env %s: %q is not an integer", key, raw)
	}
	return value, nil
}
