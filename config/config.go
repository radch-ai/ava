// Package config provides configuration loading for the gateway.
package config

import (
	"fmt"
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

	return &Config{
		ServerAddr:     addr,
		LogLevel:       logLevel,
		AllowedOrigins: origins,
		MaxConns:       maxConns,
	}, nil
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.ServerAddr == "" {
		return fmt.Errorf("server address cannot be empty")
	}
	return nil
}
