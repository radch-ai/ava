// Package utils provides small shared helpers (env access, logging setup).
// It is helpers only — per-binary config binding lives in each cmd.
package utils

import (
	"os"

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
