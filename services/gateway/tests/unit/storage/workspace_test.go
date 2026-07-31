package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/nevaria-ai/gateway/internal/storage"
)

func TestLoadWorkspaceFromAgentStore(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	userID := "user-123"
	agentID := "agent-456"

	config := &storage.AgentConfig{
		AgentID:    agentID,
		UserID:     userID,
		Name:       "TestAgent",
		SoulMd:     "# Soul\nYou are helpful.",
		AgentsMd:   "# Instructions\nBe concise.",
		UserMd:     "User is a developer.",
		IdentityMd: "Name: TestBot",
		ToolsMd:    "Use tools wisely.",
	}
	_ = store.Upsert(ctx, config)

	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	workspace, err := loader.Load(ctx, userID, agentID)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if workspace.Soul != config.SoulMd {
		t.Errorf("expected Soul %q, got %q", config.SoulMd, workspace.Soul)
	}
	if workspace.Agents != config.AgentsMd {
		t.Errorf("expected Agents %q, got %q", config.AgentsMd, workspace.Agents)
	}
	if workspace.User != config.UserMd {
		t.Errorf("expected User %q, got %q", config.UserMd, workspace.User)
	}
	if workspace.Identity != config.IdentityMd {
		t.Errorf("expected Identity %q, got %q", config.IdentityMd, workspace.Identity)
	}
	if workspace.Tools != config.ToolsMd {
		t.Errorf("expected Tools %q, got %q", config.ToolsMd, workspace.Tools)
	}
	if !workspace.Loaded {
		t.Error("expected Loaded to be true")
	}
}

func TestLoadWorkspaceFromAgentStoreNotFound(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	workspace, err := loader.Load(ctx, "user-123", "agent-456")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if workspace.Soul != "" {
		t.Error("expected empty Soul for non-existent agent")
	}
	if workspace.Loaded {
		t.Error("expected Loaded to be false for non-existent agent")
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
