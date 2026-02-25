package unit

import (
	"os"
	"testing"

	"github.com/nevaria-ai/gateway/config"
)

func TestLoadFromEnv(t *testing.T) {
	// Set environment variables
	_ = os.Setenv("GATEWAY_ADDR", ":9000")
	_ = os.Setenv("GATEWAY_LOG_LEVEL", "debug")
	defer func() {
		_ = os.Unsetenv("GATEWAY_ADDR")
		_ = os.Unsetenv("GATEWAY_LOG_LEVEL")
		_ = os.Unsetenv("GATEWAY_ALLOWED_ORIGINS")
		_ = os.Unsetenv("GATEWAY_MAX_CONNS")
	}()

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
	// Clear env
	_ = os.Unsetenv("GATEWAY_ADDR")
	_ = os.Unsetenv("GATEWAY_LOG_LEVEL")
	_ = os.Unsetenv("GATEWAY_ALLOWED_ORIGINS")
	_ = os.Unsetenv("GATEWAY_MAX_CONNS")

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

	// Empty origins means deny all (secure default)
	if len(cfg.AllowedOrigins) != 0 {
		t.Errorf("expected empty origins, got %v", cfg.AllowedOrigins)
	}

	// MaxConns default to 0 (unlimited)
	if cfg.MaxConns != 0 {
		t.Errorf("expected max conns 0, got %d", cfg.MaxConns)
	}
}

func TestValidate(t *testing.T) {
	cfg := &config.Config{
		ServerAddr: "",
		LogLevel:   "info",
	}

	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty addr, got nil")
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
			_ = os.Setenv("GATEWAY_ALLOWED_ORIGINS", tt.env)
			defer func() { _ = os.Unsetenv("GATEWAY_ALLOWED_ORIGINS") }()

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
			_ = os.Setenv("GATEWAY_MAX_CONNS", tt.env)
			defer func() { _ = os.Unsetenv("GATEWAY_MAX_CONNS") }()

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

func TestValidateValidConfig(t *testing.T) {
	cfg := &config.Config{
		ServerAddr:     ":8080",
		LogLevel:       "info",
		AllowedOrigins: []string{"*"},
		MaxConns:       1000,
		RedisAddr:      "localhost:6379",
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestZetidalConfigDefaults(t *testing.T) {
	// Clear zetidal-related env vars
	_ = os.Unsetenv("ZETIDAL_URL")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.ZetidalURL != "http://localhost:8081" {
		t.Errorf("expected zetidal URL http://localhost:8081, got %s", cfg.ZetidalURL)
	}
}

func TestZetidalConfigFromEnv(t *testing.T) {
	_ = os.Setenv("ZETIDAL_URL", "http://zetidal:9090")
	defer func() {
		_ = os.Unsetenv("ZETIDAL_URL")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.ZetidalURL != "http://zetidal:9090" {
		t.Errorf("expected zetidal URL http://zetidal:9090, got %s", cfg.ZetidalURL)
	}
}
