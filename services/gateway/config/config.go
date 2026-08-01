// Package config provides configuration loading for the gateway.
package config

import (
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
)

// A Config holds the application configuration.
type Config struct {
	ServerAddr     string
	LogLevel       string
	AllowedOrigins []string
	MaxConns       int
	ZitadelDomain  string // Zitadel instance URL for OAuth token introspection
	ZitadelKeyPath string // Path to service account JSON key file for gateway authentication
	RedisAddr      string
	RedisPassword  string
	BifrostBaseURL string // Bifrost LLM gateway URL
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

// parseList parses a comma-separated list into a string slice.
// Returns nil for empty strings (deny all for origins).
func parseList(value string) []string {
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

// SetupLogger configures the global zerolog logger and returns a service-scoped logger.
// Reads log level from GATEWAY_LOG_LEVEL env var (defaults to "info").
func SetupLogger() zerolog.Logger {
	level := getEnv("GATEWAY_LOG_LEVEL", "info")

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

	return zerolog.New(os.Stderr).With().Timestamp().Str("service", "ava-gateway").Logger()
}

// Load reads configuration from environment variables and returns a Config.
// Uses logger for fatal errors on missing required configuration.
func Load(logger zerolog.Logger) *Config {
	addr := getEnv("GATEWAY_ADDR", ":8080")
	logLevel := getEnv("GATEWAY_LOG_LEVEL", "info")

	// AllowedOrigins: required - specific origins only (empty denies all, "*" allows all but warns)
	originsRaw := getEnvRequired("GATEWAY_ALLOWED_ORIGINS", logger)
	origins := parseList(originsRaw)

	// Default connection limit (10000):
	// - Handles typical traffic spikes
	// - Prevents accidental resource exhaustion
	maxConns := 10000 // hard default
	if maxConnsStr := os.Getenv("GATEWAY_MAX_CONNS"); maxConnsStr != "" {
		var err error
		maxConns, err = strconv.Atoi(maxConnsStr)
		if err != nil {
			logger.Fatal().Str("value", maxConnsStr).Err(err).Msg("invalid GATEWAY_MAX_CONNS")
		}
		if maxConns <= 0 {
			logger.Fatal().Str("value", maxConnsStr).Msg("GATEWAY_MAX_CONNS must be a positive integer")
		}
	}

	// Redis is required for gateway services (user config store, quota system)
	redisAddr := getEnvRequired("REDIS_ADDR", logger)
	redisPassword := getEnvRequired("REDIS_PASSWORD", logger)

	// Zitadel configuration is required for auth
	zitadelDomain := getEnvRequired("ZITADEL_DOMAIN", logger)
	parsedURL, err := url.Parse(zitadelDomain)
	if err != nil || parsedURL.Scheme == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		logger.Fatal().Str("value", zitadelDomain).Msg("ZITADEL_DOMAIN must be a valid URL with http or https scheme")
	}

	zitadelKeyPath := getEnvRequired("ZITADEL_KEY_PATH", logger)

	// Bifrost LLM gateway URL (optional, defaults to localhost)
	bifrostBaseURL := getEnv("BIFROST_BASEURL", "http://localhost:8080/v1")

	// Database URL (required for user config loading)
	databaseURL := getEnvRequired("DATABASE_URL", logger)

	return &Config{
		ServerAddr:     addr,
		LogLevel:       logLevel,
		AllowedOrigins: origins,
		MaxConns:       maxConns,
		ZitadelDomain:  zitadelDomain,
		ZitadelKeyPath: zitadelKeyPath,
		RedisAddr:      redisAddr,
		RedisPassword:  redisPassword,
		BifrostBaseURL: bifrostBaseURL,
		DatabaseURL:    databaseURL,
	}
}
