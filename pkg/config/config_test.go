package config

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func TestGetEnv(t *testing.T) {
	t.Run("returns value when set", func(t *testing.T) {
		t.Setenv("AVA_TEST_GETENV", "hello")
		if got := GetEnv("AVA_TEST_GETENV", "default"); got != "hello" {
			t.Errorf("expected hello, got %s", got)
		}
	})

	t.Run("returns default when unset", func(t *testing.T) {
		_ = os.Unsetenv("AVA_TEST_GETENV_MISSING")
		if got := GetEnv("AVA_TEST_GETENV_MISSING", "default"); got != "default" {
			t.Errorf("expected default, got %s", got)
		}
	})

	t.Run("returns default when empty", func(t *testing.T) {
		t.Setenv("AVA_TEST_GETENV_EMPTY", "")
		if got := GetEnv("AVA_TEST_GETENV_EMPTY", "default"); got != "default" {
			t.Errorf("expected default for empty value, got %s", got)
		}
	})
}

func TestLoadBase(t *testing.T) {
	t.Setenv("AVA_TEST_ADDR", ":9999")

	base := LoadBase("AVA_TEST")

	if base.ServerAddr != ":9999" {
		t.Errorf("expected addr :9999, got %s", base.ServerAddr)
	}
}

func TestLoadBaseDefaults(t *testing.T) {
	_ = os.Unsetenv("AVA_TEST_DEFAULT_ADDR")

	base := LoadBase("AVA_TEST_DEFAULT")

	if base.ServerAddr != ":8080" {
		t.Errorf("expected default addr :8080, got %s", base.ServerAddr)
	}
}

func TestSetupLogger(t *testing.T) {
	t.Setenv("AVA_TEST_LOG_LEVEL", "info")
	logger := SetupLogger("ava-test", "AVA_TEST")
	// Verify we can use it without panic. (Level is set on the global zerolog
	// state, not on the returned logger, so we don't assert on logger.GetLevel().)
	logger.Info().Msg("test logger works")
}

func TestSetupLogger_SetsGlobalLevel(t *testing.T) {
	t.Setenv("AVA_TEST_LOG_LEVEL", "warn")

	_ = SetupLogger("ava-test", "AVA_TEST")

	if zerolog.GlobalLevel() != zerolog.WarnLevel {
		t.Errorf("global zerolog level = %v, want %v", zerolog.GlobalLevel(), zerolog.WarnLevel)
	}
}
