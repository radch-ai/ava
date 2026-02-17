package session_test

import (
	"context"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/session"
	"github.com/rs/zerolog"
)

func TestNewSession(t *testing.T) {
	ctx := context.Background()
	userID := "user-123"
	agentID := "agent-456"

	s := session.NewSession(ctx, userID, agentID)

	if s.SessionID() == "" {
		t.Error("expected non-empty SessionID")
	}
	if s.UserID() != userID {
		t.Errorf("expected UserID %s, got %s", userID, s.UserID())
	}
	if s.AgentID() != agentID {
		t.Errorf("expected AgentID %s, got %s", agentID, s.AgentID())
	}
	if s.Mem0SessionID() == "" {
		t.Error("expected non-empty Mem0SessionID")
	}
}

func TestSessionAddMessage(t *testing.T) {
	ctx := context.Background()
	s := session.NewSession(ctx, "user-123", "agent-456")

	msg := session.Message{
		Role:      "user",
		Content:   "Hello",
		Timestamp: time.Now().Unix(),
	}

	s.AddMessage(msg)

	messages := s.Messages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Content != "Hello" {
		t.Errorf("expected content 'Hello', got %s", messages[0].Content)
	}
}

func TestSessionLastActivity(t *testing.T) {
	ctx := context.Background()
	s := session.NewSession(ctx, "user-123", "agent-456")

	initialActivity := s.LastActivity()
	if initialActivity.IsZero() {
		t.Error("expected non-zero LastActivity")
	}

	// Wait a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	s.UpdateActivity()

	newActivity := s.LastActivity()
	if !newActivity.After(initialActivity) {
		t.Error("expected LastActivity to be updated")
	}
}

func TestSessionIsExpired(t *testing.T) {
	ctx := context.Background()
	s := session.NewSession(ctx, "user-123", "agent-456")

	// Fresh session should not be expired
	if s.IsExpired(5 * time.Minute) {
		t.Error("fresh session should not be expired")
	}

	// Manually set old activity to simulate expiration
	s.UpdateActivity()
	oldActivity := s.LastActivity()
	s.SetLastActivity(oldActivity.Add(-10 * time.Minute))

	if !s.IsExpired(5 * time.Minute) {
		t.Error("old session should be expired")
	}
}

func TestSessionManagerGetOrCreate(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	mgr := session.NewManager(logger, 5*time.Minute)

	userID := "user-123"
	agentID := "agent-456"

	// Create session
	s1 := mgr.GetOrCreateSession(ctx, userID, agentID)
	if s1 == nil {
		t.Fatal("expected non-nil session")
	}

	// Same agent should return same session (if not expired)
	s2 := mgr.GetOrCreateSession(ctx, userID, agentID)
	if s1.SessionID() != s2.SessionID() {
		t.Error("expected same session for same agent")
	}
}

func TestSessionManagerRemove(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	mgr := session.NewManager(logger, 5*time.Minute)

	sessionID := "test-session"
	userID := "user-123"
	agentID := "agent-456"

	// Add a session directly
	_ = session.NewSession(ctx, userID, agentID)
	// Use internal method to add session
	mgr.RemoveSession(sessionID) // Should not panic

	// Create via manager
	s1 := mgr.GetOrCreateSession(ctx, userID, agentID)
	sid := s1.SessionID()

	// Remove it
	mgr.RemoveSession(sid)

	// Should create new session
	s2 := mgr.GetOrCreateSession(ctx, userID, agentID)
	if s2.SessionID() == sid {
		t.Error("expected new session after removal")
	}
}

func TestSessionManagerCleanup(t *testing.T) {
	ctx := context.Background()
	logger := zerolog.Nop()
	mgr := session.NewManager(logger, 5*time.Minute)

	userID := "user-123"
	agentID := "agent-456"

	// Create session
	s := mgr.GetOrCreateSession(ctx, userID, agentID)

	// Manually expire it
	s.SetLastActivity(s.LastActivity().Add(-10 * time.Minute))

	// Run cleanup
	count := mgr.CleanupExpired()
	if count != 1 {
		t.Errorf("expected to clean up 1 session, got %d", count)
	}
}
