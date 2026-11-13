// Package agent_test provides tests for the agent runtime.
package agent_test

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/gateway/internal/agent"
)

func TestUserAgentManager_CreatesWithDefaults(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	require.NotNil(t, mgr)
}
