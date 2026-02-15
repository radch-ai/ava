// Package proxy_test provides tests for the proxy client implementation.
package proxy_test

import (
	"context"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	llm_proxy "github.com/nevaria-ai/gateway/internal/llm-proxy"
)

// TestProxyClientImplementsLLMClient verifies that Client implements agent.LLMClient.
func TestProxyClientImplementsLLMClient(t *testing.T) {
	var _ agent.LLMClient = (*llm_proxy.Client)(nil)
}

// TestNewClient verifies client creation with handler and userID.
func TestNewClient(t *testing.T) {
	handler := llm_proxy.NewHandler(llm_proxy.HandlerOptions{
		LLMClient: &mockLLMClientForProxy{},
	})
	userID := "test-user-123"

	client := llm_proxy.NewClient(handler, userID)

	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

// TestClientStream verifies that Stream method correctly calls handler.
func TestClientStream(t *testing.T) {
	// Create mock event channel
	eventCh := make(chan agent.AgentEvent, 3)

	// Create mock handler that returns our test channel
	mockLLM := &mockLLMClientForProxy{
		eventCh: eventCh,
	}

	handler := llm_proxy.NewHandler(llm_proxy.HandlerOptions{
		LLMClient: mockLLM,
	})

	userID := "test-user-123"
	client := llm_proxy.NewClient(handler, userID)

	ctx := context.Background()

	// Prepare test messages
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Hello, agent!"),
	}
	tools := []agent.Tool{}

	// Start streaming in background
	go func() {
		// Send test events
		eventCh <- agent.MessageStartEvent{
			Timestamp: time.Now().Unix(),
			Message:   agent.NewTextMessage(agent.RoleAssistant, "Hello!"),
		}
		eventCh <- agent.MessageUpdateEvent{
			Timestamp: time.Now().Unix(),
			Message:   agent.NewTextMessage(agent.RoleAssistant, "Hello!"),
			Delta:     "Hello!",
		}
		eventCh <- agent.MessageEndEvent{
			Timestamp: time.Now().Unix(),
			Message:   agent.NewTextMessage(agent.RoleAssistant, "Hello!"),
		}
		close(eventCh)
	}()

	// Call Stream
	resultCh, err := client.Stream(ctx, messages, tools)
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	// Verify we receive events
	eventCount := 0
	for range resultCh {
		eventCount++
	}

	if eventCount != 3 {
		t.Errorf("Expected 3 events, got %d", eventCount)
	}
}

// mockLLMClientForProxy is a minimal mock for testing Client
type mockLLMClientForProxy struct {
	eventCh <-chan agent.AgentEvent
}

func (m *mockLLMClientForProxy) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	return m.eventCh, nil
}
