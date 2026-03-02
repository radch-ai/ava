package unit

import (
	"os"
	"strings"
	"testing"

	"github.com/nevaria-ai/gateway/config"
)

// Set required env vars for tests that need successful load
func setRequiredEnv() {
	_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", "https://example.com")
	_ = os.Setenv("REDIS_ADDR", "localhost:6379")
	_ = os.Setenv("REDIS_PASSWORD", "test-password")
	_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
	_ = os.Setenv("ZITADEL_KEY_PATH", "/path/to/key.json")
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
}

func TestLoadFromEnv(t *testing.T) {
	clearEnv()
	setRequiredEnv()
	_ = os.Setenv("GATEWAY_ADDR", ":9000")
	_ = os.Setenv("GATEWAY_LOG_LEVEL", "debug")
	defer clearEnv()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

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

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.ServerAddr != ":8080" {
		t.Errorf("expected default addr :8080, got %s", cfg.ServerAddr)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("expected default log level info, got %s", cfg.LogLevel)
	}

	// MaxConns default to 0 (unlimited)
	if cfg.MaxConns != 0 {
		t.Errorf("expected max conns 0, got %d", cfg.MaxConns)
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

			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("failed to load config: %v", err)
			}

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
		wantErr  bool
	}{
		{
			name:     "valid max connections",
			env:      "5000",
			expected: 5000,
			wantErr:  false,
		},
		{
			name:     "zero max connections",
			env:      "0",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "negative max connections",
			env:      "-1",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "invalid max connections",
			env:      "abc",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "large max connections",
			env:      "100000",
			expected: 100000,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			setRequiredEnv()
			_ = os.Setenv("GATEWAY_MAX_CONNS", tt.env)
			defer clearEnv()

			cfg, err := config.Load()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.MaxConns != tt.expected {
				t.Errorf("expected max conns %d, got %d", tt.expected, cfg.MaxConns)
			}
		})
	}
}

func TestRequiredFields(t *testing.T) {
	tests := []struct {
		name        string
		setup       func()
		errContains string
	}{
		{
			name: "missing GATEWAY_ALLOWED_ORIGINS",
			setup: func() {
				_ = os.Setenv("REDIS_ADDR", "localhost:6379")
				_ = os.Setenv("REDIS_PASSWORD", "test-password")
				_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
				_ = os.Setenv("ZITADEL_KEY_PATH", "/path/to/key.json")
			},
			errContains: "GATEWAY_ALLOWED_ORIGINS is required",
		},
		{
			name: "missing REDIS_ADDR",
			setup: func() {
				_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", "https://example.com")
				_ = os.Setenv("REDIS_PASSWORD", "test-password")
				_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
				_ = os.Setenv("ZITADEL_KEY_PATH", "/path/to/key.json")
			},
			errContains: "REDIS_ADDR is required",
		},
		{
			name: "missing REDIS_PASSWORD",
			setup: func() {
				_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", "https://example.com")
				_ = os.Setenv("REDIS_ADDR", "localhost:6379")
				_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
				_ = os.Setenv("ZITADEL_KEY_PATH", "/path/to/key.json")
			},
			errContains: "REDIS_PASSWORD is required",
		},
		{
			name: "missing ZITADEL_DOMAIN",
			setup: func() {
				_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", "https://example.com")
				_ = os.Setenv("REDIS_ADDR", "localhost:6379")
				_ = os.Setenv("REDIS_PASSWORD", "test-password")
				_ = os.Setenv("ZITADEL_KEY_PATH", "/path/to/key.json")
			},
			errContains: "ZITADEL_DOMAIN is required",
		},
		{
			name: "missing ZITADEL_KEY_PATH",
			setup: func() {
				_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", "https://example.com")
				_ = os.Setenv("REDIS_ADDR", "localhost:6379")
				_ = os.Setenv("REDIS_PASSWORD", "test-password")
				_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
			},
			errContains: "ZITADEL_KEY_PATH is required",
		},
		{
			name: "invalid ZITADEL_DOMAIN URL",
			setup: func() {
				_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", "https://example.com")
				_ = os.Setenv("REDIS_ADDR", "localhost:6379")
				_ = os.Setenv("REDIS_PASSWORD", "test-password")
				_ = os.Setenv("ZITADEL_DOMAIN", "not-a-url")
				_ = os.Setenv("ZITADEL_KEY_PATH", "/path/to/key.json")
			},
			errContains: "ZITADEL_DOMAIN must be a valid URL",
		},
		{
			name: "ZITADEL_DOMAIN without scheme",
			setup: func() {
				_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", "https://example.com")
				_ = os.Setenv("REDIS_ADDR", "localhost:6379")
				_ = os.Setenv("REDIS_PASSWORD", "test-password")
				_ = os.Setenv("ZITADEL_DOMAIN", "example.zitadel.cloud")
				_ = os.Setenv("ZITADEL_KEY_PATH", "/path/to/key.json")
			},
			errContains: "ZITADEL_DOMAIN must be a valid URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			tt.setup()
			defer clearEnv()

			_, err := config.Load()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error to contain %q, got %q", tt.errContains, err.Error())
			}
		})
	}
}

func TestValidConfig(t *testing.T) {
	clearEnv()
	setRequiredEnv()
	defer clearEnv()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

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
