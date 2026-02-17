package agent_test

import (
	"github.com/nevaria-ai/gateway/internal/agent"
)

// extractToolTextResult extracts text from a tool result.
// This helper function is shared across all tool tests.
func extractToolTextResult(result *agent.ToolResult) string {
	if len(result.Content) == 0 {
		return ""
	}
	if tc, ok := result.Content[0].(agent.TextContent); ok {
		return tc.Text
	}
	return ""
}
