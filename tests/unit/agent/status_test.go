package agent_test

import (
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

func TestAgentStatusValues(t *testing.T) {
	statuses := []agent.AgentStatus{
		agent.AgentStatusIdle,
		agent.AgentStatusActive,
		agent.AgentStatusError,
	}

	for _, status := range statuses {
		if status.String() == "" {
			t.Errorf("status %v has empty string representation", status)
		}
	}
}

func TestAgentStatusString(t *testing.T) {
	tests := []struct {
		status   agent.AgentStatus
		expected string
	}{
		{agent.AgentStatusIdle, "idle"},
		{agent.AgentStatusActive, "active"},
		{agent.AgentStatusError, "error"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.expected {
			t.Errorf("AgentStatus.String() = %v, want %v", got, tt.expected)
		}
	}
}
