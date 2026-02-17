package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nevaria-ai/gateway/internal/storage"
)

func TestLoadWorkspace(t *testing.T) {
	client := storage.NewMockS3Client()
	ctx := context.Background()

	userID := "user-123"
	agentID := "agent-456"

	// Pre-populate workspace files
	_ = client.Put(ctx, storage.WorkspacePath(userID, agentID, "SOUL.md"), []byte("# Soul\nYou are helpful."))
	_ = client.Put(ctx, storage.WorkspacePath(userID, agentID, "AGENTS.md"), []byte("# Instructions\nBe concise."))

	loader := storage.NewWorkspaceLoader(client)

	workspace, err := loader.Load(ctx, userID, agentID)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if workspace.Soul == "" {
		t.Error("expected non-empty Soul")
	}
	if !strings.Contains(workspace.Soul, "helpful") {
		t.Errorf("expected Soul to contain 'helpful', got %s", workspace.Soul)
	}

	if workspace.Agents == "" {
		t.Error("expected non-empty Agents")
	}
}

func TestLoadWorkspaceEmpty(t *testing.T) {
	client := storage.NewMockS3Client()
	ctx := context.Background()

	loader := storage.NewWorkspaceLoader(client)

	workspace, err := loader.Load(ctx, "user-123", "agent-456")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Should return empty workspace without error
	if workspace.Soul != "" {
		t.Error("expected empty Soul for non-existent workspace")
	}
}

func TestWorkspaceFormatSystemPrompt(t *testing.T) {
	workspace := &storage.Workspace{
		Soul:     "# Soul\nYou are helpful.",
		Agents:   "# Instructions\nBe concise.",
		User:     "User is a developer.",
		Identity: "Name: Claude",
		Tools:    "Use tools wisely.",
	}

	prompt := workspace.FormatSystemPrompt()

	if !strings.Contains(prompt, "helpful") {
		t.Error("expected prompt to contain Soul content")
	}
	if !strings.Contains(prompt, "concise") {
		t.Error("expected prompt to contain Agents content")
	}
	if !strings.Contains(prompt, "developer") {
		t.Error("expected prompt to contain User section")
	}
	if !strings.Contains(prompt, "Claude") {
		t.Error("expected prompt to contain Identity")
	}
	if !strings.Contains(prompt, "tools") {
		t.Error("expected prompt to contain Tools guidance")
	}
}
