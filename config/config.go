// Package config provides configuration loading for the backend.
package config

import (
	"net/url"
	"os"

	"github.com/rs/zerolog"
)

// Config holds the application configuration.
type Config struct {
	ServerAddr     string
	LogLevel       string
	ZitadelDomain                   string // Zitadel instance URL for OAuth token introspection
	ZitadelIntrospectorPrivateKeyPath string // Path to application key JSON file for Private Key JWT auth
	ZitadelWebhookSigningKey        string // Signing key for Zitadel Actions v2 webhook signature verification (optional)
	DatabaseURL    string // PostgreSQL connection URL
}

// getEnv returns the environment variable value or a default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvRequired returns the environment variable value or fatals with logger.
func getEnvRequired(key string, logger zerolog.Logger) string {
	value := os.Getenv(key)
	if value == "" {
		logger.Fatal().Str("env_var", key).Msg("required environment variable is not set")
	}
	return value
}

// SetupLogger configures the global zerolog logger and returns a service-scoped logger.
// Reads log level from BACKEND_LOG_LEVEL env var (defaults to "info").
func SetupLogger() zerolog.Logger {
	level := getEnv("BACKEND_LOG_LEVEL", "info")

	logLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		// Use stderr directly since logger isn't set up yet
		logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
		logger.Fatal().
			Str("level", level).
			Err(err).
			Msg("invalid log level")
	}
	zerolog.SetGlobalLevel(logLevel)

	return zerolog.New(os.Stderr).With().Timestamp().Str("service", "nevaria-backend").Logger()
}

// Load reads configuration from environment variables and returns a Config.
// Uses logger for fatal errors on missing required configuration.
func Load(logger zerolog.Logger) *Config {
	addr := getEnv("BACKEND_ADDR", ":8080")
	logLevel := getEnv("BACKEND_LOG_LEVEL", "info")

	// Zitadel configuration is required for auth
	zitadelDomain := getEnvRequired("ZITADEL_DOMAIN", logger)
	parsedURL, err := url.Parse(zitadelDomain)
	if err != nil || parsedURL.Scheme == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		logger.Fatal().Str("value", zitadelDomain).Msg("ZITADEL_DOMAIN must be a valid URL with http or https scheme")
	}

	zitadelIntrospectorPrivateKeyPath := getEnvRequired("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH", logger)

	// Database URL is required
	databaseURL := getEnvRequired("DATABASE_URL", logger)

	// Zitadel webhook signing key is required for Actions v2 signature verification
	zitadelWebhookSigningKey := getEnvRequired("ZITADEL_WEBHOOK_SIGNING_KEY", logger)

	return &Config{
		ServerAddr:                       addr,
		LogLevel:                         logLevel,
		ZitadelDomain:                    zitadelDomain,
		ZitadelIntrospectorPrivateKeyPath: zitadelIntrospectorPrivateKeyPath,
		ZitadelWebhookSigningKey:         zitadelWebhookSigningKey,
		DatabaseURL:                      databaseURL,
	}
}
