package mem0_test

import (
	"context"
	"strings"
	"testing"

	"github.com/radch-ai/ava/services/gateway/internal/mem0"
)

func TestMockMem0ClientSearch(t *testing.T) {
	client := mem0.NewMockClient()
	ctx := context.Background()
	userID := "user-123"

	// Add some memories
	_ = client.Add(ctx, userID, "User prefers Python", "")
	_ = client.Add(ctx, userID, "User is working on AI project", "")

	// Search memories
	results, err := client.Search(ctx, userID, "Python")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected at least one memory result")
	}

	found := false
	for _, r := range results {
		if strings.Contains(r, "Python") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find memory about Python")
	}
}

func TestMockMem0ClientAdd(t *testing.T) {
	client := mem0.NewMockClient()
	ctx := context.Background()
	userID := "user-123"

	err := client.Add(ctx, userID, "Test memory", "")
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify memory was added
	results, _ := client.Search(ctx, userID, "Test")
	if len(results) == 0 {
		t.Error("expected to find added memory")
	}
}

func TestMockMem0ClientSessionScoped(t *testing.T) {
	client := mem0.NewMockClient()
	ctx := context.Background()
	userID := "user-123"
	runID := "session-456"

	// Add session-scoped memory
	err := client.Add(ctx, userID, "Session context", runID)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Search in session scope
	results, err := client.SearchInSession(ctx, userID, runID, "Session")
	if err != nil {
		t.Fatalf("SearchInSession failed: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected to find session memory")
	}
}

func TestMockMem0ClientCreateSession(t *testing.T) {
	client := mem0.NewMockClient()
	ctx := context.Background()
	userID := "user-123"

	sessionID, err := client.CreateSession(ctx, userID)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if sessionID == "" {
		t.Error("expected non-empty session ID")
	}
}
