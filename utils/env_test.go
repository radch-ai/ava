package utils

import (
	"os"
	"testing"
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
