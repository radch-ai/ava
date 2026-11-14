package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
)

// BuildSystemPrompt pins: the persona renders as ordered, headed
// sections — Soul (personality before identity), Operating
// Instructions, User Information, Tool Guidance — and empty sections
// never reach the prompt.

func textPtr(s string) *string { return &s }

func TestBuildSystemPromptOrdersAllSections(t *testing.T) {
	cfg := &apitypes.AgentConfig{
		AgentEditableFields: apitypes.AgentEditableFields{
			Soul:                  &apitypes.SoulConfig{Personality: "You are warm.", Identity: "A research companion."},
			OperatingInstructions: textPtr("Follow the house rules."),
			UserContext:           textPtr("Lives in Lyon."),
			ToolsContext:          textPtr("Prefer search."),
		},
	}
	require.Equal(t,
		"## Soul\n\nYou are warm.\n\nA research companion.\n\n"+
			"## Operating Instructions\n\nFollow the house rules.\n\n"+
			"## User Information\n\nLives in Lyon.\n\n"+
			"## Tool Guidance\n\nPrefer search.",
		agent.BuildSystemPrompt(cfg))
}

func TestBuildSystemPromptSkipsEmptySections(t *testing.T) {
	// Identity only: the personality slot is absent, the identity
	// still leads under the Soul header.
	cfg := &apitypes.AgentConfig{
		AgentEditableFields: apitypes.AgentEditableFields{
			Soul: &apitypes.SoulConfig{Identity: "A research companion."},
		},
	}
	require.Equal(t, "## Soul\n\nA research companion.", agent.BuildSystemPrompt(cfg))

	// One headed section: everything else stays out.
	cfg = &apitypes.AgentConfig{
		AgentEditableFields: apitypes.AgentEditableFields{
			UserContext: textPtr("Lives in Lyon."),
		},
	}
	require.Equal(t, "## User Information\n\nLives in Lyon.", agent.BuildSystemPrompt(cfg))
}

func TestBuildSystemPromptEmptyConfigBuildsEmptyPrompt(t *testing.T) {
	require.Equal(t, "", agent.BuildSystemPrompt(&apitypes.AgentConfig{}))
	require.Equal(t, "", agent.BuildSystemPrompt(nil))
}
