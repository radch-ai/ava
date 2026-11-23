// Package agent_test provides tests for the agent runtime.
package agent_test

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/gateway/internal/agent"
)

// Constructed directly: the constructor's default behavior is the
// subject, not incidental wiring for a scenario.
func TestUserAgentManager_CreatesWithDefaults(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{GetAgentThread: emptyThread})

	require.NotNil(t, mgr)
}
