package agent_test

import (
	"context"
	"strings"
	"testing"

	agenttools "github.com/nevaria-ai/gateway/internal/agent/tools"
	"github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/mem0"
)

func TestMem0SearchTool(t *testing.T) {
	client := mem0.NewMockClient()
	userID := "user-123"

	// Add a memory
	_ = client.Add(context.Background(), userID, "User likes Go programming", "")

	// Create context with userID
	requestCtx := ctx.WithUserID(context.Background(), userID)

	// Create tool
	tool := agenttools.Mem0SearchTool(client)

	// Execute tool
	result, err := tool.Execute(requestCtx, map[string]any{"query": "Go"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultText := extractToolTextResult(result)
	if !strings.Contains(resultText, "Go") {
		t.Errorf("expected result to contain 'Go', got %s", resultText)
	}
}

func TestMem0StoreTool(t *testing.T) {
	client := mem0.NewMockClient()
	userID := "user-123"

	requestCtx := ctx.WithUserID(context.Background(), userID)

	// Create tool
	tool := agenttools.Mem0StoreTool(client)

	// Execute tool
	result, err := tool.Execute(requestCtx, map[string]any{"content": "User prefers dark mode"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify the result confirms storage
	resultText := extractToolTextResult(result)
	if !strings.Contains(resultText, "Stored:") {
		t.Errorf("expected confirmation of storage, got %s", resultText)
	}

	// Verify memory was stored
	memories, _ := client.Search(requestCtx, userID, "dark mode")
	if len(memories) == 0 {
		t.Error("expected to find stored memory")
	}
}
