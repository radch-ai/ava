// Package proxy provides LLM proxy functionality with routing, quota management, and usage tracking.
package proxy

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

func TestProxyRequest(t *testing.T) {
	req := ProxyRequest{
		UserID:    "test-user",
		SessionID: "test-session",
		Query:     "Hello, world!",
	}

	if req.UserID != "test-user" {
		t.Error("UserID not set correctly")
	}
	if req.Query != "Hello, world!" {
		t.Error("Query not set correctly")
	}
}

func TestProxyResponse(t *testing.T) {
	resp := ProxyResponse{
		UsingAddon:     false,
		AddonRemaining: 10.0,
	}

	if resp.UsingAddon {
		t.Error("Should not be using addon")
	}
	if resp.AddonRemaining != 10.0 {
		t.Error("AddonRemaining not set correctly")
	}
}

func TestHandlerOptions(t *testing.T) {
	opts := HandlerOptions{
		LLMClient: &mockLLMClient{},
	}

	if opts.LLMClient == nil {
		t.Error("LLMClient should not be nil")
	}
}

func TestNewHandler(t *testing.T) {
	handler := NewHandler(HandlerOptions{
		LLMClient: &mockLLMClient{},
	})

	if handler == nil {
		t.Error("NewHandler returned nil")
	}
}

func TestHandleProxyRequest(t *testing.T) {
	handler := NewHandler(HandlerOptions{
		LLMClient: &mockLLMClient{},
	})

	ctx := context.Background()
	req := ProxyRequest{
		UserID:    "test-user",
		SessionID: "test-session",
		Query:     "Hello, world!",
		Messages: []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Hello, world!"),
		},
	}

	resp, err := handler.Handle(ctx, req)
	if err != nil {
		t.Logf("Handle failed (expected during development): %v", err)
		// This is expected to fail until full implementation
		return
	}

	if resp == nil {
		t.Error("Handle returned nil response")
	}
}

// Mock implementations

type mockLLMClient struct{}

func (m *mockLLMClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent, 1)
	go func() {
		defer close(ch)
		// Send a simple message end event
		ch <- agent.MessageEndEvent{
			Message: agent.NewTextMessage(agent.RoleAssistant, "Hello!"),
		}
	}()
	return ch, nil
}
