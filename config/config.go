// Package config provides configuration loading for the gateway.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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

// Load reads configuration from environment variables and returns a Config.
func Load() (*Config, error) {
	addr := os.Getenv("GATEWAY_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	logLevel := os.Getenv("GATEWAY_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	// Parse allowed origins from comma-separated env var
	originsEnv := os.Getenv("GATEWAY_ALLOWED_ORIGINS")
	var origins []string
	if originsEnv != "" {
		origins = strings.Split(originsEnv, ",")
		// Trim whitespace from each origin
		for i, o := range origins {
			origins[i] = strings.TrimSpace(o)
		}
	}
	// NOTE: Empty origins means deny all - requires explicit configuration for security.
	// Use GATEWAY_ALLOWED_ORIGINS="*" for development only.

	// Parse max connections (default 0 = unlimited)
	maxConns := 0
	maxConnsStr := os.Getenv("GATEWAY_MAX_CONNS")
	if maxConnsStr != "" {
		var err error
		maxConns, err = strconv.Atoi(maxConnsStr)
		if err != nil || maxConns < 0 {
			return nil, fmt.Errorf("invalid GATEWAY_MAX_CONNS: %q", maxConnsStr)
		}
	}

	// Load auth configuration
	authURL := os.Getenv("AUTH_SERVICE_URL")
	if authURL == "" {
		authURL = "http://localhost:8081"
	}

	authTimeout := 2 * time.Second
	authTimeoutStr := os.Getenv("AUTH_TIMEOUT")
	if authTimeoutStr != "" {
		var err error
		authTimeout, err = time.ParseDuration(authTimeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid AUTH_TIMEOUT: %q", authTimeoutStr)
		}
	}

	internalSecret := os.Getenv("INTERNAL_SECRET")

	// Redis configuration (optional)
	redisAddr := os.Getenv("REDIS_ADDR")
	redisPassword := os.Getenv("REDIS_PASSWORD")

	return &Config{
		ServerAddr:     addr,
		LogLevel:       logLevel,
		AllowedOrigins: origins,
		MaxConns:       maxConns,
		Auth: AuthConfig{
			URL:     authURL,
			Timeout: authTimeout,
			Secret:  internalSecret,
		},
		RedisAddr:     redisAddr,
		RedisPassword: redisPassword,
	}, nil
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.ServerAddr == "" {
		return fmt.Errorf("server address cannot be empty")
	}
	return nil
}
