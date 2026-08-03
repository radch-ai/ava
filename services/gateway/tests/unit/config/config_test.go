package unit

import (
	"os"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/services/gateway/config"
)

// Set required env vars for tests that need successful load
func setRequiredEnv() {
	_ = os.Setenv("REDIS_ADDR", "localhost:6379")
	_ = os.Setenv("REDIS_PASSWORD", "test-password")
	_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
	_ = os.Setenv("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH", "/path/to/key.json")
	_ = os.Setenv("BIFROST_BASEURL", "http://localhost:8080/v1")
	_ = os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb")
}

// Clear all gateway-related env vars
func clearEnv() {
	_ = os.Unsetenv("GATEWAY_ADDR")
	_ = os.Unsetenv("GATEWAY_LOG_LEVEL")
	_ = os.Unsetenv("GATEWAY_MAX_CONNS")
	_ = os.Unsetenv("REDIS_ADDR")
	_ = os.Unsetenv("REDIS_PASSWORD")
	_ = os.Unsetenv("ZITADEL_DOMAIN")
	_ = os.Unsetenv("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH")
	_ = os.Unsetenv("BIFROST_BASEURL")
	_ = os.Unsetenv("DATABASE_URL")
}

func TestLoadFromEnv(t *testing.T) {
	clearEnv()
	setRequiredEnv()
	_ = os.Setenv("GATEWAY_ADDR", ":9000")
	defer clearEnv()

	cfg := config.Load(zerolog.Nop())

	if cfg.ServerAddr != ":9000" {
		t.Errorf("expected addr :9000, got %s", cfg.ServerAddr)
	}
}

func TestDefaultValues(t *testing.T) {
	clearEnv()
	setRequiredEnv()
	defer clearEnv()

	cfg := config.Load(zerolog.Nop())

	if cfg.ServerAddr != ":8080" {
		t.Errorf("expected default addr :8080, got %s", cfg.ServerAddr)
	}

	// MaxConns defaults to 10000
	if cfg.MaxConns != 10000 {
		t.Errorf("expected max conns 10000, got %d", cfg.MaxConns)
	}
}

func TestLoadMaxConns(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected int
	}{
		{
			name:     "valid max connections",
			env:      "5000",
			expected: 5000,
		},
		{
			name:     "large max connections",
			env:      "100000",
			expected: 100000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			setRequiredEnv()
			_ = os.Setenv("GATEWAY_MAX_CONNS", tt.env)
			defer clearEnv()

			cfg := config.Load(zerolog.Nop())

			if cfg.MaxConns != tt.expected {
				t.Errorf("expected max conns %d, got %d", tt.expected, cfg.MaxConns)
			}
		})
	}
}

func TestValidConfig(t *testing.T) {
	clearEnv()
	setRequiredEnv()
	defer clearEnv()

	cfg := config.Load(zerolog.Nop())

	if cfg.RedisAddr != "localhost:6379" {
		t.Errorf("expected RedisAddr localhost:6379, got %s", cfg.RedisAddr)
	}
	if cfg.RedisPassword != "test-password" {
		t.Errorf("expected RedisPassword test-password, got %s", cfg.RedisPassword)
	}
	if cfg.ZitadelDomain != "https://example.zitadel.cloud" {
		t.Errorf("expected ZitadelDomain https://example.zitadel.cloud, got %s", cfg.ZitadelDomain)
	}
	if cfg.ZitadelKeyPath != "/path/to/key.json" {
		t.Errorf("expected ZitadelKeyPath /path/to/key.json, got %s", cfg.ZitadelKeyPath)
	}
	if cfg.BifrostBaseURL != "http://localhost:8080/v1" {
		t.Errorf("expected BifrostBaseURL http://localhost:8080/v1, got %s", cfg.BifrostBaseURL)
	}
}
