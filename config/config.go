// Package config provides configuration loading for the gateway.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
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
}

// getEnv returns the environment variable value or a default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvRequired returns the environment variable value or an error if not set.
func getEnvRequired(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
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

// Load reads configuration from environment variables and returns a Config.
// Returns an error if any required environment variable is missing or invalid.
func Load() (*Config, error) {
	addr := getEnv("GATEWAY_ADDR", ":8080")
	logLevel := getEnv("GATEWAY_LOG_LEVEL", "info")

	// AllowedOrigins: required - specific origins only (empty denies all, "*" allows all but warns)
	originsRaw, err := getEnvRequired("GATEWAY_ALLOWED_ORIGINS")
	if err != nil {
		return nil, err
	}
	origins := parseList(originsRaw)

	// Default connection limit (10000):
	// - Handles typical traffic spikes
	// - Prevents accidental resource exhaustion
	maxConns := 10000 // hard default
	if maxConnsStr := os.Getenv("GATEWAY_MAX_CONNS"); maxConnsStr != "" {
		var err error
		maxConns, err = strconv.Atoi(maxConnsStr)
		if err != nil {
			return nil, fmt.Errorf("invalid GATEWAY_MAX_CONNS: %q", maxConnsStr)
		}
		if maxConns <= 0 {
			return nil, fmt.Errorf("invalid GATEWAY_MAX_CONNS: must be a positive integer")
		}
	}

	// Redis is required for gateway services (user config store, quota system)
	redisAddr, err := getEnvRequired("REDIS_ADDR")
	if err != nil {
		return nil, err
	}
	redisPassword, err := getEnvRequired("REDIS_PASSWORD")
	if err != nil {
		return nil, err
	}

	// Zitadel configuration is required for auth
	zitadelDomain, err := getEnvRequired("ZITADEL_DOMAIN")
	if err != nil {
		return nil, err
	}
	parsedURL, err := url.Parse(zitadelDomain)
	if err != nil || parsedURL.Scheme == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return nil, fmt.Errorf("ZITADEL_DOMAIN must be a valid URL with http or https scheme")
	}

	zitadelKeyPath, err := getEnvRequired("ZITADEL_KEY_PATH")
	if err != nil {
		return nil, err
	}

	return &Config{
		ServerAddr:     addr,
		LogLevel:       logLevel,
		AllowedOrigins: origins,
		MaxConns:       maxConns,
		ZitadelDomain:  zitadelDomain,
		ZitadelKeyPath: zitadelKeyPath,
		RedisAddr:      redisAddr,
		RedisPassword:  redisPassword,
	}, nil
}
