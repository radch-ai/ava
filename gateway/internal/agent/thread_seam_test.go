package agent_test

import (
	"context"

	"github.com/radch-ai/ava/backend"
)

// emptyThread is the thread seam for manager tests that run turns without
// asserting model input, so the scripted reply drives the assertions. Tests
// that assert what the model received wire the real store
// (turn_context_test.go, turn_prompt_test.go).
func emptyThread(context.Context, string) ([]backend.AgentThreadMessage, error) {
	return nil, nil
}
