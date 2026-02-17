package ctx_test

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/ctx"
)

func TestWithAgentID(t *testing.T) {
	baseCtx := context.Background()
	agentCtx := ctx.WithAgentID(baseCtx, "agent-123")

	agentID := ctx.GetAgentIDFromContext(agentCtx)
	if agentID != "agent-123" {
		t.Errorf("expected agent-123, got %s", agentID)
	}
}

func TestGetAgentIDFromContextMissing(t *testing.T) {
	agentID := ctx.GetAgentIDFromContext(context.Background())
	if agentID != "" {
		t.Errorf("expected empty string, got %s", agentID)
	}
}
