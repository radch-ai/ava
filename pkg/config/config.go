// Package config provides shared configuration primitives used by Ava services.
//
// Env var convention:
//   - Service-level vars (per-instance values like addr, log level, allowed
//     origins) are prefixed with the service name.
//   - External dependencies that are the same regardless of which service
//     connects (DATABASE_URL, ZITADEL_*) are unprefixed; services read them
//     directly from their own config package.
//   - When two services need different instances of the same kind of
//     dependency, the var is namespaced to be distinguishable. This mirrors
//     the case of one service having multiple instances (or environments like
//     dev/staging/prod) of the same dependency, extended across services.
//     Disambiguation can be by prefix (BACKEND_DATABASE_URL,
//     GATEWAY_DATABASE_URL) or any other distinguishing naming — the
//     requirement is just that the names not collide.
package config

import (
	"os"
	"strings"

	"github.com/rs/zerolog"
)

// BaseConfig holds the server-level fields common to every service.
type BaseConfig struct {
	ServerAddr     string
	AllowedOrigins []string
}

// LoadBase reads <prefix>_ADDR and <prefix>_ALLOWED_ORIGINS from env.
func LoadBase(prefix string, logger zerolog.Logger) BaseConfig {
	addr := GetEnv(prefix+"_ADDR", ":8080")
	originsRaw := GetEnvRequired(prefix+"_ALLOWED_ORIGINS", logger)

	return BaseConfig{
		ServerAddr:     addr,
		AllowedOrigins: ParseList(originsRaw),
	}
}

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

// ParseList parses a comma-separated list into a string slice.
// Returns nil for empty strings (deny all for origins).
func ParseList(value string) []string {
	if value == "" {
		return nil
	}
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

// SetupLogger configures the global zerolog level from <prefix>_LOG_LEVEL and
// returns a logger tagged with the service name.
func SetupLogger(service, prefix string) zerolog.Logger {
	level := GetEnv(prefix+"_LOG_LEVEL", "info")

	logLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		// Configured logger isn't constructed yet; use a throwaway one to report.
		logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
		logger.Fatal().
			Str("level", level).
			Err(err).
			Msg("invalid log level")
	}
	zerolog.SetGlobalLevel(logLevel)

	return zerolog.New(os.Stderr).With().Timestamp().Str("service", service).Logger()
}
