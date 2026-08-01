package unit

import (
	"os"
	"testing"

	"github.com/rs/zerolog"

	"github.com/radch-ai/ava/services/gateway/config"
)

// Set required env vars for tests that need successful load
func setRequiredEnv() {
	_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", "https://example.com")
	_ = os.Setenv("REDIS_ADDR", "localhost:6379")
	_ = os.Setenv("REDIS_PASSWORD", "test-password")
	_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
	_ = os.Setenv("ZITADEL_KEY_PATH", "/path/to/key.json")
	_ = os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb")
}

// Clear all gateway-related env vars
func clearEnv() {
	_ = os.Unsetenv("GATEWAY_ADDR")
	_ = os.Unsetenv("GATEWAY_LOG_LEVEL")
	_ = os.Unsetenv("GATEWAY_ALLOWED_ORIGINS")
	_ = os.Unsetenv("GATEWAY_MAX_CONNS")
	_ = os.Unsetenv("REDIS_ADDR")
	_ = os.Unsetenv("REDIS_PASSWORD")
	_ = os.Unsetenv("ZITADEL_DOMAIN")
	_ = os.Unsetenv("ZITADEL_KEY_PATH")
	_ = os.Unsetenv("DATABASE_URL")
}

func TestLoadFromEnv(t *testing.T) {
	clearEnv()
	setRequiredEnv()
	_ = os.Setenv("GATEWAY_ADDR", ":9000")
	_ = os.Setenv("GATEWAY_LOG_LEVEL", "debug")
	defer clearEnv()

	cfg := config.Load(zerolog.Nop())

	if cfg.ServerAddr != ":9000" {
		t.Errorf("expected addr :9000, got %s", cfg.ServerAddr)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("expected log level debug, got %s", cfg.LogLevel)
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

	if cfg.LogLevel != "info" {
		t.Errorf("expected default log level info, got %s", cfg.LogLevel)
	}

	// MaxConns defaults to 10000
	if cfg.MaxConns != 10000 {
		t.Errorf("expected max conns 10000, got %d", cfg.MaxConns)
	}
}

func TestLoadAllowedOrigins(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected []string
	}{
		{
			name:     "single origin",
			env:      "https://example.com",
			expected: []string{"https://example.com"},
		},
		{
			name:     "multiple origins with comma",
			env:      "https://example.com,https://test.com",
			expected: []string{"https://example.com", "https://test.com"},
		},
		{
			name:     "origins with spaces trimmed",
			env:      "https://example.com , https://test.com  ",
			expected: []string{"https://example.com", "https://test.com"},
		},
		{
			name:     "wildcard origin",
			env:      "*",
			expected: []string{"*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			setRequiredEnv()
			_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", tt.env)
			defer clearEnv()

			cfg := config.Load(zerolog.Nop())

			if len(cfg.AllowedOrigins) != len(tt.expected) {
				t.Errorf("expected %d origins, got %d", len(tt.expected), len(cfg.AllowedOrigins))
			}

			for i, origin := range cfg.AllowedOrigins {
				if origin != tt.expected[i] {
					t.Errorf("origin %d: expected %s, got %s", i, tt.expected[i], origin)
				}
			}
		})
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
}

func TestSetupLogger(t *testing.T) {
	// Test that SetupLogger returns a valid logger
	logger := config.SetupLogger()
	// Verify we can use it without panic
	logger.Info().Msg("test logger works")
}
