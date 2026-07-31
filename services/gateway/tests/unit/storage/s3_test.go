package storage_test

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/storage"
)

func TestTranscriptPath(t *testing.T) {
	userID := "user-123"
	agentID := "agent-456"
	sessionID := "session-789"

	path := storage.TranscriptPath(userID, agentID, sessionID)
	expected := "user-123/agent-456/transcripts/session-789.jsonl"

	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestMockS3Client(t *testing.T) {
	client := storage.NewMockS3Client()
	ctx := context.Background()

	userID := "user-123"
	agentID := "agent-456"

	transcriptPath := storage.TranscriptPath(userID, agentID, "session-789")
	line1 := "{\"turn\":1,\"user\":\"hello\"}\n"
	line2 := "{\"turn\":2,\"assistant\":\"hi\"}\n"

	err := client.Append(ctx, transcriptPath, []byte(line1))
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	err = client.Append(ctx, transcriptPath, []byte(line2))
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	result, err := client.Get(ctx, transcriptPath)
	if err != nil {
		t.Fatalf("Get transcript failed: %v", err)
	}

	expected := line1 + line2
	if string(result) != expected {
		t.Errorf("expected %s, got %s", expected, string(result))
	}
}
