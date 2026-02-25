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
	Auth           AuthConfig
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

// parseList parses a comma-separated list into a string slice.
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
func Load() (*Config, error) {
	addr := getEnv("GATEWAY_ADDR", ":8080")
	logLevel := getEnv("GATEWAY_LOG_LEVEL", "info")
	origins := parseList(os.Getenv("GATEWAY_ALLOWED_ORIGINS"))

	// NOTE: Empty origins means deny all - requires explicit configuration for security.
	// Use GATEWAY_ALLOWED_ORIGINS="*" for development only.

	// Parse max connections (default 0 = unlimited) - validate strictly
	maxConns := 0
	maxConnsStr := os.Getenv("GATEWAY_MAX_CONNS")
	if maxConnsStr != "" {
		var err error
		maxConns, err = strconv.Atoi(maxConnsStr)
		if err != nil {
			return nil, fmt.Errorf("invalid GATEWAY_MAX_CONNS: %q", maxConnsStr)
		}
	}
	if maxConns < 0 {
		return nil, fmt.Errorf("invalid GATEWAY_MAX_CONNS: must be non-negative")
	}

	// Load zetidal configuration
	zetidalURL := getEnv("ZETIDAL_URL", "http://localhost:8081")

	// Redis configuration (required)
	redisAddr := os.Getenv("REDIS_ADDR")
	redisPassword := os.Getenv("REDIS_PASSWORD")

	return &Config{
		ServerAddr:     addr,
		LogLevel:       logLevel,
		AllowedOrigins: origins,
		MaxConns:       maxConns,
		Auth: AuthConfig{
			URL: zetidalURL,
		},
		RedisAddr:     redisAddr,
		RedisPassword: redisPassword,
	}, nil
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	var errs []string

	if c.ServerAddr == "" {
		errs = append(errs, "server address cannot be empty")
	}

	// Redis is required
	if c.RedisAddr == "" {
		errs = append(errs, "REDIS_ADDR is required")
	}

	// Validate zetidal URL format - must have http or https scheme
	if c.Auth.URL != "" {
		parsedURL, err := url.Parse(c.Auth.URL)
		if err != nil || parsedURL.Scheme == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			errs = append(errs, "ZETIDAL_URL must be a valid URL with http or https scheme")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed: %s", strings.Join(errs, "; "))
	}

	return nil
}
