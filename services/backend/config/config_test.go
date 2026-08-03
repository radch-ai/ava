package config

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

// Set required env vars for tests that need successful load.
func setRequiredEnv() {
	_ = os.Setenv("ZITADEL_DOMAIN", "https://example.zitadel.cloud")
	_ = os.Setenv("ZITADEL_INTROSPECTOR_PRIVATE_KEY_PATH", "/path/to/key.json")
	_ = os.Setenv("ZITADEL_WEBHOOK_SIGNING_KEY", "test-signing-key")
	_ = os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/testdb")
}

// Clear all backend-related env vars.
func clearEnv() {
	_ = os.Unsetenv("BACKEND_ADDR")
	_ = os.Unsetenv("BACKEND_LOG_LEVEL")
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
