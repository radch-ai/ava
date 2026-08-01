package config

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

// Set required env vars for tests that need successful load.
func setRequiredEnv() {
	_ = os.Setenv("BACKEND_ALLOWED_ORIGINS", "http://localhost:4173")
	_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
	_ = os.Setenv("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH", "/path/to/key.json")
	_ = os.Setenv("ZITADEL_WEBHOOK_SIGNING_KEY", "test-signing-key")
	_ = os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb")
}

// Clear all backend-related env vars.
func clearEnv() {
	_ = os.Unsetenv("BACKEND_ADDR")
	_ = os.Unsetenv("BACKEND_LOG_LEVEL")
	_ = os.Unsetenv("BACKEND_ALLOWED_ORIGINS")
	_ = os.Unsetenv("ZITADEL_DOMAIN")
	_ = os.Unsetenv("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH")
	_ = os.Unsetenv("ZITADEL_WEBHOOK_SIGNING_KEY")
	_ = os.Unsetenv("DATABASE_URL")
}

func TestLoadFromEnv(t *testing.T) {
	clearEnv()
	setRequiredEnv()
	_ = os.Setenv("BACKEND_ADDR", ":9000")
	defer clearEnv()

	cfg := Load(zerolog.Nop())

	if cfg.ServerAddr != ":9000" {
		t.Errorf("expected addr :9000, got %s", cfg.ServerAddr)
	}
}

func TestDefaultValues(t *testing.T) {
	clearEnv()
	setRequiredEnv()
	defer clearEnv()

	cfg := Load(zerolog.Nop())

	if cfg.ServerAddr != ":8080" {
		t.Errorf("expected default addr :8080, got %s", cfg.ServerAddr)
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
			env:      "http://localhost:4173",
			expected: []string{"http://localhost:4173"},
		},
		{
			name:     "multiple origins with comma",
			env:      "http://localhost:4173,http://host.docker.internal:4173",
			expected: []string{"http://localhost:4173", "http://host.docker.internal:4173"},
		},
		{
			name:     "origins with spaces trimmed",
			env:      "http://localhost:4173 , http://host.docker.internal:4173  ",
			expected: []string{"http://localhost:4173", "http://host.docker.internal:4173"},
		},
		{
			name:     "whitespace-only entries ignored",
			env:      "http://localhost:4173,  ,http://host.docker.internal:4173",
			expected: []string{"http://localhost:4173", "http://host.docker.internal:4173"},
		},
		{
			name:     "whitespace-only env results in empty slice",
			env:      "   ",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			setRequiredEnv()
			_ = os.Setenv("BACKEND_ALLOWED_ORIGINS", tt.env)
			defer clearEnv()

			cfg := Load(zerolog.Nop())

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

func TestValidConfig(t *testing.T) {
	clearEnv()
	setRequiredEnv()
	defer clearEnv()

	cfg := Load(zerolog.Nop())

	if cfg.ZitadelDomain != "https://example.zitadel.cloud" {
		t.Errorf("expected ZitadelDomain https://example.zitadel.cloud, got %s", cfg.ZitadelDomain)
	}
	if cfg.ZitadelIntrospectorPrivateKeyPath != "/path/to/key.json" {
		t.Errorf("expected ZitadelIntrospectorPrivateKeyPath /path/to/key.json, got %s", cfg.ZitadelIntrospectorPrivateKeyPath)
	}
	if cfg.ZitadelWebhookSigningKey != "test-signing-key" {
		t.Errorf("expected ZitadelWebhookSigningKey test-signing-key, got %s", cfg.ZitadelWebhookSigningKey)
	}
	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/testdb" {
		t.Errorf("expected DatabaseURL, got %s", cfg.DatabaseURL)
	}
}
