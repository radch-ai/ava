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

func TestParseList(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		expected []string
	}{
		{
			name:     "empty returns nil",
			env:      "",
			expected: nil,
		},
		{
			name:     "single entry",
			env:      "http://localhost:4173",
			expected: []string{"http://localhost:4173"},
		},
		{
			name:     "multiple entries with comma",
			env:      "http://localhost:4173,http://host.docker.internal:4173",
			expected: []string{"http://localhost:4173", "http://host.docker.internal:4173"},
		},
		{
			name:     "entries trimmed",
			env:      "http://localhost:4173 , http://host.docker.internal:4173  ",
			expected: []string{"http://localhost:4173", "http://host.docker.internal:4173"},
		},
		{
			name:     "whitespace-only entries ignored",
			env:      "http://localhost:4173,  ,http://host.docker.internal:4173",
			expected: []string{"http://localhost:4173", "http://host.docker.internal:4173"},
		},
		{
			name:     "whitespace-only env returns nil",
			env:      "   ",
			expected: nil,
		},
		{
			name:     "wildcard preserved",
			env:      "*",
			expected: []string{"*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseList(tt.env)
			if len(got) != len(tt.expected) {
				t.Errorf("expected %d entries, got %d (%v)", len(tt.expected), len(got), got)
				return
			}
			for i, v := range got {
				if v != tt.expected[i] {
					t.Errorf("entry %d: expected %s, got %s", i, tt.expected[i], v)
				}
			}
		})
	}
}

func TestLoadBase(t *testing.T) {
	t.Setenv("AVA_TEST_ADDR", ":9999")
	t.Setenv("AVA_TEST_ALLOWED_ORIGINS", "https://a.com, https://b.com")

	base := LoadBase("AVA_TEST", zerolog.Nop())

	if base.ServerAddr != ":9999" {
		t.Errorf("expected addr :9999, got %s", base.ServerAddr)
	}
	if len(base.AllowedOrigins) != 2 || base.AllowedOrigins[0] != "https://a.com" {
		t.Errorf("unexpected origins: %v", base.AllowedOrigins)
	}
}

func TestLoadBaseDefaults(t *testing.T) {
	_ = os.Unsetenv("AVA_TEST_DEFAULT_ADDR")
	t.Setenv("AVA_TEST_DEFAULT_ALLOWED_ORIGINS", "https://example.com")

	base := LoadBase("AVA_TEST_DEFAULT", zerolog.Nop())

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
