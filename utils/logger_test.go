package utils

import (
	"testing"

	"github.com/rs/zerolog"
)

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
