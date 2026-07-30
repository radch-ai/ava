package config

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func setRequiredEnv() {
	_ = os.Setenv("ALLOWED_ORIGINS", "http://localhost:4173")
	_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
	_ = os.Setenv("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH", "/path/to/key.json")
	_ = os.Setenv("ZITADEL_WEBHOOK_SIGNING_KEY", "test-signing-key")
	_ = os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb")
}

func clearEnv() {
	_ = os.Unsetenv("ADDR")
	_ = os.Unsetenv("LOG_LEVEL")
	_ = os.Unsetenv("ALLOWED_ORIGINS")
	_ = os.Unsetenv("ZITADEL_DOMAIN")
	_ = os.Unsetenv("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH")
	_ = os.Unsetenv("ZITADEL_WEBHOOK_SIGNING_KEY")
	_ = os.Unsetenv("DATABASE_URL")
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
			_ = os.Setenv("ALLOWED_ORIGINS", tt.env)
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
