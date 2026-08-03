// Package config provides shared configuration primitives used by Ava services.
//
// Env var convention:
//   - Service-level vars (per-instance values like addr, log level) are
//     prefixed with the service name.
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

	"github.com/rs/zerolog"
)

// BaseConfig holds the server-level fields common to every service.
type BaseConfig struct {
	ServerAddr string
}

// LoadBase reads <prefix>_ADDR from env (default ":8080").
func LoadBase(prefix string) BaseConfig {
	return BaseConfig{
		ServerAddr: GetEnv(prefix+"_ADDR", ":8080"),
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
