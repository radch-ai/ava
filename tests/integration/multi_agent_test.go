//go:build integration
// +build integration

package integration_test

import (
	"context"
	"path"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	ctxpkg "github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/nevaria-ai/gateway/internal/mem0"
	"github.com/nevaria-ai/gateway/internal/session"
	"github.com/nevaria-ai/gateway/internal/storage"
	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/rs/zerolog"
)

func workspacePath(userID, agentID, filename string) string {
	return path.Join(userID, agentID, "workspace", filename)
}

// TestMultiAgentPerUser verifies multiple agents can be created for a single user.
func TestMultiAgentPerUser(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "user-123"

	// Create three different agents for the same user
	agent1 := mgr.GetOrCreateAgentForUser(userID, "agent-1")
	agent2 := mgr.GetOrCreateAgentForUser(userID, "agent-2")
	agent3 := mgr.GetOrCreateAgentForUser(userID, "agent-3")

	// Verify they are different
	if agent1 == agent2 {
		t.Error("agent1 and agent2 should be different")
	}
	if agent2 == agent3 {
		t.Error("agent2 and agent3 should be different")
	}

	// Verify state has correct agent IDs
	state1 := agent1.State()
	if state1.AgentID != "agent-1" {
		t.Errorf("expected agent-1, got %s", state1.AgentID)
	}

	state2 := agent2.State()
	if state2.AgentID != "agent-2" {
		t.Errorf("expected agent-2, got %s", state2.AgentID)
	}
}

// TestSessionIsolation verifies sessions are isolated per agent.
func TestSessionIsolation(t *testing.T) {
	ctx := context.Background()
	sessionMgr := session.NewManager(zerolog.Nop(), 5*time.Minute)

	userID := "user-123"
	agentID1 := "agent-1"
	agentID2 := "agent-2"

	// Create sessions for different agents
	session1 := sessionMgr.GetOrCreateSession(ctx, userID, agentID1)
	session2 := sessionMgr.GetOrCreateSession(ctx, userID, agentID2)

	if session1.SessionID() == session2.SessionID() {
		t.Error("sessions should have different IDs")
	}

	if session1.AgentID() != agentID1 {
		t.Errorf("expected agent-1, got %s", session1.AgentID())
	}

	if session2.AgentID() != agentID2 {
		t.Errorf("expected agent-2, got %s", session2.AgentID())
	}
}

// TestMemoryIsolation verifies memories are isolated per user.
func TestMemoryIsolation(t *testing.T) {
	ctx := context.Background()
	client := mem0.NewMockClient()

	user1 := "user-1"
	user2 := "user-2"

	// Add memory for user1
	_ = client.Add(ctx, user1, "User1 likes pizza", "")

	// Add memory for user2
	_ = client.Add(ctx, user2, "User2 hates pizza", "")

	// Search user1 memories
	results1, _ := client.Search(ctx, user1, "pizza")
	if len(results1) != 1 {
		t.Errorf("expected 1 result for user1, got %d", len(results1))
	}

	// Search user2 memories
	results2, _ := client.Search(ctx, user2, "pizza")
	if len(results2) != 1 {
		t.Errorf("expected 1 result for user2, got %d", len(results2))
	}

	// Verify content is different
	if results1[0] == results2[0] {
		t.Error("memories should be different for different users")
	}
}

// TestStorageIsolation verifies S3 paths enforce isolation.
func TestStorageIsolation(t *testing.T) {
	ctx := context.Background()
	client := storage.NewMockS3Client()

	userID := "user-123"
	agentID := "agent-456"

	// Write a file for this user/agent
	filePath := workspacePath(userID, agentID, "SOUL.md")
	_ = client.Put(ctx, filePath, []byte("Agent personality"))

	// Try to read with correct path
	data, err := client.Get(ctx, filePath)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if string(data) != "Agent personality" {
		t.Errorf("expected 'Agent personality', got %s", string(data))
	}

	// Verify different user/agent can't access (different path)
	differentPath := workspacePath("other-user", "other-agent", "SOUL.md")
	_, err = client.Get(ctx, differentPath)
	if err == nil {
		t.Error("expected error when accessing different user's file")
	}
}

// TestAgentWithTools verifies agents can have tools added.
func TestAgentWithTools(t *testing.T) {
	opts := agent.AgentOptions{
		UserID:  "user-123",
		AgentID: "agent-456",
	}
	a := agent.NewAgent(opts)

	// Create a test tool
	testTool := agent.Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: "test result"}},
			}, nil
		},
	}

	// Add tool
	a.AddTools(testTool)

	// Verify tool is in state
	state := a.State()
	if len(state.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(state.Tools))
	}

	if state.Tools[0].Name != "test_tool" {
		t.Errorf("expected tool name 'test_tool', got %s", state.Tools[0].Name)
	}
}

// TestSessionTimeout verifies sessions timeout correctly.
func TestSessionTimeout(t *testing.T) {
	ctx := context.Background()
	s := session.NewSession(ctx, "user-123", "agent-456")

	// Fresh session should not be expired
	if s.IsExpired(5 * time.Minute) {
		t.Error("fresh session should not be expired")
	}

	// Set old activity time
	oldTime := time.Now().Add(-10 * time.Minute)
	s.SetLastActivity(oldTime)

	// Should be expired now
	if !s.IsExpired(5 * time.Minute) {
		t.Error("old session should be expired")
	}
}

// TestWorkspaceLoaderWithAgentStore verifies workspace loading from AgentStore.
func TestWorkspaceLoaderWithAgentStore(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMockAgentStore()
	loader := storage.NewWorkspaceLoaderFromAgentStore(store)

	userID := "user-123"
	agentID := "agent-456"

	// Pre-populate agent config
	config := &storage.AgentConfig{
		AgentID:  agentID,
		UserID:   userID,
		Name:     "TestAgent",
		SoulMd:   "# Soul\nYou are helpful.",
		AgentsMd: "# Instructions\nBe concise.",
	}
	_ = store.Upsert(ctx, config)

	// Load workspace
	workspace, err := loader.Load(ctx, userID, agentID)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify loaded content
	prompt := workspace.FormatSystemPrompt()
	if len(prompt) == 0 {
		t.Error("expected non-empty system prompt")
	}
}

// TestContextWithIDs verifies context utilities work correctly.
func TestContextWithIDs(t *testing.T) {
	ctx := context.Background()

	userID := "user-123"
	agentID := "agent-456"

	// Add IDs to context
	ctx = ctxpkg.WithUserID(ctx, userID)
	ctx = ctxpkg.WithAgentID(ctx, agentID)

	// Retrieve IDs
	retrievedUserID := ctxpkg.GetUserIDFromContext(ctx)
	retrievedAgentID := ctxpkg.GetAgentIDFromContext(ctx)

	if retrievedUserID != userID {
		t.Errorf("expected userID %s, got %s", userID, retrievedUserID)
	}

	if retrievedAgentID != agentID {
		t.Errorf("expected agentID %s, got %s", agentID, retrievedAgentID)
	}
}
