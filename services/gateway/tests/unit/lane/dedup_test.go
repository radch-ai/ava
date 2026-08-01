package lane_test

import (
	"testing"

	"github.com/radch-ai/ava/services/gateway/internal/lane"
)

func TestDedupCache_ResponseCache(t *testing.T) {
	sessionID := "test-session-123"
	messageID := "msg-456"
	responseData := []byte(`{"content":"test response","finished":true}`)

	dedup := lane.NewDedup()

	err := dedup.StoreResponse(sessionID, messageID, responseData)
	if err != nil {
		t.Fatalf("StoreResponse failed: %v", err)
	}

	retrieved := dedup.GetResponse(sessionID, messageID)
	if retrieved == nil {
		t.Fatal("GetResponse returned nil for cached data")
	}

	if string(retrieved) != string(responseData) {
		t.Errorf("GetResponse returned different data: got %s, want %s", string(retrieved), string(responseData))
	}
}
