// Package agent_test provides tests for agent tools.
package agent_test

import (
	"context"
	"path"
	"strings"
	"testing"

	agenttools "github.com/nevaria-ai/gateway/internal/agent/tools"
	"github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/storage"
)

func workspacePath(userID, agentID, filename string) string {
	return path.Join(userID, agentID, "workspace", filename)
}

func TestReadAgentFileTool(t *testing.T) {
	client := storage.NewMockS3Client()
	userID := "user-123"
	agentID := "agent-456"

	// Pre-populate a file
	bgCtx := context.Background()
	_ = client.Put(bgCtx, workspacePath(userID, agentID, "SOUL.md"), []byte("Test content"))

	// Create context with userID and agentID
	requestCtx := ctx.WithUserID(bgCtx, userID)
	requestCtx = ctx.WithAgentID(requestCtx, agentID)

	// Create tool
	tool := agenttools.ReadAgentFileTool(client)

	// Execute tool
	result, err := tool.Execute(requestCtx, map[string]any{"path": "SOUL.md"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	resultText := extractToolTextResult(result)
	if !strings.Contains(resultText, "Test content") {
		t.Errorf("expected result to contain 'Test content', got %s", resultText)
	}
}

func TestWriteAgentFileTool(t *testing.T) {
	client := storage.NewMockS3Client()
	userID := "user-123"
	agentID := "agent-456"

	bgCtx := context.Background()
	requestCtx := ctx.WithUserID(bgCtx, userID)
	requestCtx = ctx.WithAgentID(requestCtx, agentID)

	// Create tool
	tool := agenttools.WriteAgentFileTool(client)

	// Execute tool
	_, err := tool.Execute(requestCtx, map[string]any{
		"path":    "CUSTOM.md",
		"content": "Custom content",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify file was written
	data, err := client.Get(bgCtx, workspacePath(userID, agentID, "CUSTOM.md"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(data) != "Custom content" {
		t.Errorf("expected 'Custom content', got %s", string(data))
	}
}

func TestReadAgentFileToolEnforcesScope(t *testing.T) {
	client := storage.NewMockS3Client()

	// Create context with different user/agent
	bgCtx := context.Background()
	requestCtx := ctx.WithUserID(bgCtx, "other-user")
	requestCtx = ctx.WithAgentID(requestCtx, "other-agent")

	tool := agenttools.ReadAgentFileTool(client)

	// Try to read a file from different user/agent
	result, err := tool.Execute(requestCtx, map[string]any{"path": "SOUL.md"})
	// Should either fail or return not found
	if err == nil {
		// If no error, verify empty result or error in content
		content := extractToolTextResult(result)
		if !strings.Contains(content, "Error") && content != "" {
			t.Error("expected error or empty result for different user/agent")
		}
	}
}

func TestReadAgentFileToolRequiresContext(t *testing.T) {
	client := storage.NewMockS3Client()
	ctx := context.Background()

	tool := agenttools.ReadAgentFileTool(client)

	// No userID or agentID in context
	_, err := tool.Execute(ctx, map[string]any{"path": "SOUL.md"})
	if err == nil {
		t.Error("expected error when userID/agentID not in context")
	}
}
