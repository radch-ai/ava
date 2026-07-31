package storage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/radch-ai/ava/services/gateway/internal/agent"
	"github.com/radch-ai/ava/services/gateway/internal/storage"
)

func TestWriteTurn(t *testing.T) {
	client := storage.NewMockS3Client()
	writer := storage.NewTranscriptWriter(client)
	ctx := context.Background()

	userID := "user-123"
	agentID := "agent-456"
	sessionID := "session-789"

	// Create a turn
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Hello"),
		agent.NewTextMessage(agent.RoleAssistant, "Hi there"),
	}

	turn := storage.Turn{
		SessionID:  sessionID,
		UserID:     userID,
		AgentID:    agentID,
		TurnNumber: 1,
		Messages:   messages,
		Timestamp:  time.Now().Unix(),
	}

	err := writer.WriteTurn(ctx, turn)
	if err != nil {
		t.Fatalf("WriteTurn failed: %v", err)
	}

	// Verify the transcript was written
	path := storage.TranscriptPath(userID, agentID, sessionID)
	data, err := client.Get(ctx, path)
	if err != nil {
		t.Fatalf("Get transcript failed: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "Hello") {
		t.Errorf("expected transcript to contain 'Hello', got %s", content)
	}
}

func TestTurnToJSONL(t *testing.T) {
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "test"),
	}

	turn := storage.Turn{
		SessionID:  "session-123",
		UserID:     "user-123",
		AgentID:    "agent-123",
		TurnNumber: 1,
		Messages:   messages,
		Timestamp:  1234567890,
	}

	line := storage.TurnToJSONL(turn)

	if !strings.HasPrefix(line, "{") {
		t.Error("expected JSON line to start with {")
	}
	if !strings.Contains(line, "test") {
		t.Error("expected JSON line to contain user message")
	}
}
