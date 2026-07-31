package storage_test

import (
	"context"
	"sync"
	"testing"

	"github.com/nevaria-ai/gateway/internal/storage"
)

func TestMockAgentStore_GetNonExistent(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	config, err := store.Get(ctx, "user-123", "agent-456")
	if err == nil {
		t.Error("expected error for non-existent agent")
	}
	if config != nil {
		t.Error("expected nil config for non-existent agent")
	}
}

func TestMockAgentStore_UpsertAndGet(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	config := &storage.AgentConfig{
		AgentID:     "agent-456",
		UserID:      "user-123",
		Name:        "Test Agent",
		SoulMd:      "# Soul\nYou are helpful.",
		AgentsMd:    "# Agents\nBe concise.",
		UserMd:      "# User\nCustom user info.",
		IdentityMd:  "# Identity\nName: TestBot",
		ToolsMd:     "# Tools\nUse tools wisely.",
		HeartbeatMd: "# Heartbeat\nCheck health.",
		Config: map[string]any{
			"temperature": 0.7,
			"max_tokens":  1000,
		},
	}

	err := store.Upsert(ctx, config)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	retrieved, err := store.Get(ctx, "user-123", "agent-456")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.AgentID != config.AgentID {
		t.Errorf("expected AgentID %s, got %s", config.AgentID, retrieved.AgentID)
	}
	if retrieved.UserID != config.UserID {
		t.Errorf("expected UserID %s, got %s", config.UserID, retrieved.UserID)
	}
	if retrieved.Name != config.Name {
		t.Errorf("expected Name %s, got %s", config.Name, retrieved.Name)
	}
	if retrieved.SoulMd != config.SoulMd {
		t.Errorf("expected SoulMd %s, got %s", config.SoulMd, retrieved.SoulMd)
	}
	if retrieved.AgentsMd != config.AgentsMd {
		t.Errorf("expected AgentsMd %s, got %s", config.AgentsMd, retrieved.AgentsMd)
	}
	if retrieved.UserMd != config.UserMd {
		t.Errorf("expected UserMd %s, got %s", config.UserMd, retrieved.UserMd)
	}
	if retrieved.IdentityMd != config.IdentityMd {
		t.Errorf("expected IdentityMd %s, got %s", config.IdentityMd, retrieved.IdentityMd)
	}
	if retrieved.ToolsMd != config.ToolsMd {
		t.Errorf("expected ToolsMd %s, got %s", config.ToolsMd, retrieved.ToolsMd)
	}
	if retrieved.HeartbeatMd != config.HeartbeatMd {
		t.Errorf("expected HeartbeatMd %s, got %s", config.HeartbeatMd, retrieved.HeartbeatMd)
	}
	if retrieved.Config["temperature"] != config.Config["temperature"] {
		t.Errorf("expected Config temperature %v, got %v", config.Config["temperature"], retrieved.Config["temperature"])
	}
}

func TestMockAgentStore_UpsertUpdatesExisting(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	config := &storage.AgentConfig{
		AgentID: "agent-456",
		UserID:  "user-123",
		Name:    "Original Name",
		SoulMd:  "Original soul",
		Config:  map[string]any{"key": "original"},
	}

	err := store.Upsert(ctx, config)
	if err != nil {
		t.Fatalf("first Upsert failed: %v", err)
	}

	updatedConfig := &storage.AgentConfig{
		AgentID: "agent-456",
		UserID:  "user-123",
		Name:    "Updated Name",
		SoulMd:  "Updated soul",
		Config:  map[string]any{"key": "updated", "new_key": "new_value"},
	}

	err = store.Upsert(ctx, updatedConfig)
	if err != nil {
		t.Fatalf("second Upsert failed: %v", err)
	}

	retrieved, err := store.Get(ctx, "user-123", "agent-456")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Name != "Updated Name" {
		t.Errorf("expected Name 'Updated Name', got %s", retrieved.Name)
	}
	if retrieved.SoulMd != "Updated soul" {
		t.Errorf("expected SoulMd 'Updated soul', got %s", retrieved.SoulMd)
	}
	if retrieved.Config["key"] != "updated" {
		t.Errorf("expected Config key 'updated', got %v", retrieved.Config["key"])
	}
	if retrieved.Config["new_key"] != "new_value" {
		t.Errorf("expected Config new_key 'new_value', got %v", retrieved.Config["new_key"])
	}
}

func TestMockAgentStore_Delete(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	config := &storage.AgentConfig{
		AgentID: "agent-456",
		UserID:  "user-123",
		Name:    "Test Agent",
	}

	err := store.Upsert(ctx, config)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	err = store.Delete(ctx, "user-123", "agent-456")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	retrieved, err := store.Get(ctx, "user-123", "agent-456")
	if err == nil {
		t.Error("expected error after delete")
	}
	if retrieved != nil {
		t.Error("expected nil config after delete")
	}
}

func TestMockAgentStore_DeleteNonExistent(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	err := store.Delete(ctx, "user-123", "non-existent")
	if err == nil {
		t.Error("expected error when deleting non-existent agent")
	}
}

func TestMockAgentStore_ListByUser(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	userID := "user-123"
	otherUserID := "user-456"

	agents := []*storage.AgentConfig{
		{AgentID: "agent-1", UserID: userID, Name: "Agent 1"},
		{AgentID: "agent-2", UserID: userID, Name: "Agent 2"},
		{AgentID: "agent-3", UserID: userID, Name: "Agent 3"},
		{AgentID: "agent-4", UserID: otherUserID, Name: "Other User Agent"},
	}

	for _, cfg := range agents {
		err := store.Upsert(ctx, cfg)
		if err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	list, err := store.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser failed: %v", err)
	}

	if len(list) != 3 {
		t.Errorf("expected 3 agents for user, got %d", len(list))
	}

	for _, cfg := range list {
		if cfg.UserID != userID {
			t.Errorf("expected UserID %s, got %s", userID, cfg.UserID)
		}
	}
}

func TestMockAgentStore_ListByUserEmpty(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	list, err := store.ListByUser(ctx, "user-with-no-agents")
	if err != nil {
		t.Fatalf("ListByUser failed: %v", err)
	}

	if len(list) != 0 {
		t.Errorf("expected empty list, got %d agents", len(list))
	}
}

func TestMockAgentStore_ConcurrentAccess(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	numOps := 100

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			userID := "user-concurrent"
			agentID := "agent-concurrent"

			config := &storage.AgentConfig{
				AgentID: agentID,
				UserID:  userID,
				Name:    "Concurrent Agent",
				Config:  map[string]any{"index": idx},
			}

			_ = store.Upsert(ctx, config)
			_, _ = store.Get(ctx, userID, agentID)
		}(i)
	}

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			userID := "user-list"
			agentID := "agent-list"

			config := &storage.AgentConfig{
				AgentID: agentID,
				UserID:  userID,
				Name:    "List Agent",
			}

			_ = store.Upsert(ctx, config)
			_, _ = store.ListByUser(ctx, userID)
		}(i)
	}

	wg.Wait()

	list, err := store.ListByUser(ctx, "user-concurrent")
	if err != nil {
		t.Fatalf("final ListByUser failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 agent, got %d", len(list))
	}
}

func TestMockAgentStore_EmptyFields(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	config := &storage.AgentConfig{
		AgentID:     "agent-empty",
		UserID:      "user-empty",
		Name:        "",
		SoulMd:      "",
		AgentsMd:    "",
		UserMd:      "",
		IdentityMd:  "",
		ToolsMd:     "",
		HeartbeatMd: "",
		Config:      nil,
	}

	err := store.Upsert(ctx, config)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	retrieved, err := store.Get(ctx, "user-empty", "agent-empty")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Name != "" {
		t.Errorf("expected empty Name, got %s", retrieved.Name)
	}
	if retrieved.Config != nil {
		t.Errorf("expected nil Config, got %v", retrieved.Config)
	}
}

func TestMockAgentStore_DifferentUsersSameAgentID(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	config1 := &storage.AgentConfig{
		AgentID: "agent-same-id",
		UserID:  "user-1",
		Name:    "User 1 Agent",
	}

	config2 := &storage.AgentConfig{
		AgentID: "agent-same-id",
		UserID:  "user-2",
		Name:    "User 2 Agent",
	}

	err := store.Upsert(ctx, config1)
	if err != nil {
		t.Fatalf("Upsert config1 failed: %v", err)
	}

	err = store.Upsert(ctx, config2)
	if err != nil {
		t.Fatalf("Upsert config2 failed: %v", err)
	}

	retrieved1, err := store.Get(ctx, "user-1", "agent-same-id")
	if err != nil {
		t.Fatalf("Get user-1 failed: %v", err)
	}
	if retrieved1.Name != "User 1 Agent" {
		t.Errorf("expected Name 'User 1 Agent', got %s", retrieved1.Name)
	}

	retrieved2, err := store.Get(ctx, "user-2", "agent-same-id")
	if err != nil {
		t.Fatalf("Get user-2 failed: %v", err)
	}
	if retrieved2.Name != "User 2 Agent" {
		t.Errorf("expected Name 'User 2 Agent', got %s", retrieved2.Name)
	}

	list1, err := store.ListByUser(ctx, "user-1")
	if err != nil {
		t.Fatalf("ListByUser user-1 failed: %v", err)
	}
	if len(list1) != 1 {
		t.Errorf("expected 1 agent for user-1, got %d", len(list1))
	}

	list2, err := store.ListByUser(ctx, "user-2")
	if err != nil {
		t.Fatalf("ListByUser user-2 failed: %v", err)
	}
	if len(list2) != 1 {
		t.Errorf("expected 1 agent for user-2, got %d", len(list2))
	}
}

func TestMockAgentStore_DeleteDoesNotAffectOtherUsers(t *testing.T) {
	store := storage.NewMockAgentStore()
	ctx := context.Background()

	config1 := &storage.AgentConfig{
		AgentID: "agent-same-id",
		UserID:  "user-1",
		Name:    "User 1 Agent",
	}

	config2 := &storage.AgentConfig{
		AgentID: "agent-same-id",
		UserID:  "user-2",
		Name:    "User 2 Agent",
	}

	_ = store.Upsert(ctx, config1)
	_ = store.Upsert(ctx, config2)

	err := store.Delete(ctx, "user-1", "agent-same-id")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = store.Get(ctx, "user-1", "agent-same-id")
	if err == nil {
		t.Error("expected error for deleted agent")
	}

	retrieved2, err := store.Get(ctx, "user-2", "agent-same-id")
	if err != nil {
		t.Fatalf("Get user-2 should still work: %v", err)
	}
	if retrieved2.Name != "User 2 Agent" {
		t.Errorf("expected Name 'User 2 Agent', got %s", retrieved2.Name)
	}
}
