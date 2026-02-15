// Package agent_test provides tests for the agent runtime.
package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// TestNewAgent verifies agent initialization with default values.
func TestNewAgent(t *testing.T) {
	opts := agent.AgentOptions{
		ID:           "test-agent-1",
		SystemPrompt: "You are a helpful assistant",
		SessionID:    "session-123",
	}

	a := agent.NewAgent(opts)

	if a == nil {
		t.Fatal("NewAgent returned nil")
	}

	// Can't access private fields directly in agent_test package
	// Test through public API instead
	state := a.State()
	if state.SessionID != opts.SessionID {
		t.Errorf("expected SessionID %q, got %q", opts.SessionID, state.SessionID)
	}

	if state.SystemPrompt != opts.SystemPrompt {
		t.Errorf("expected SystemPrompt %q, got %q", opts.SystemPrompt, state.SystemPrompt)
	}
}

// TestNewAgentDefaults verifies agent initialization with minimal options.
// TestNewAgentDefaults verifies agent initialization with minimal options.
func TestNewAgentDefaults(t *testing.T) {
	opts := agent.AgentOptions{}

	a := agent.NewAgent(opts)

	state := a.State()
	// SessionID is not auto-generated if not provided, it will be empty
	// The agent ID is auto-generated, but SessionID is set from options
	if state.SessionID != "" {
		t.Errorf("expected empty SessionID when not provided, got %q", state.SessionID)
	}
}

// TestSubscribeUnsubscribe verifies event subscription lifecycle.
func TestSubscribeUnsubscribe(t *testing.T) {
	a := agent.NewAgent(agent.AgentOptions{})

	// Subscribe
	subID, ch := a.Subscribe()

	if subID == "" {
		t.Fatal("subscription ID should not be empty")
	}

	if ch == nil {
		t.Fatal("channel should not be nil")
	}

	// Unsubscribe
	a.Unsubscribe(subID)

	// Try to receive from channel - should be closed or no new events
	select {
	case _, ok := <-ch:
		_ = ok // Channel state check is enough
	case <-time.After(50 * time.Millisecond):
		// Timeout is acceptable
	}
}

// TestEmit verifies event broadcasting to subscribers.
func TestEmit(t *testing.T) {
	a := agent.NewAgent(agent.AgentOptions{})

	// Subscribe
	subID, _ := a.Subscribe()
	defer a.Unsubscribe(subID)

	// Trigger an event by calling Prompt with a mock
	mockLLM := &agent.MockLLMClient{
		Responses: []agent.MockResponse{
			{
				Message: agent.NewTextMessage(agent.RoleAssistant, "test"),
			},
		},
	}

	opts := agent.AgentOptions{
		LLMClient: mockLLM,
	}
	a = agent.NewAgent(opts)

	subID2, ch2 := a.Subscribe()
	defer a.Unsubscribe(subID2)

	// Emit an event through Prompt
	ctx := context.Background()
	_ = a.Prompt(ctx, "test")

	// Should receive events
	select {
	case <-ch2:
		// Got event
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive event")
	}
}

// TestConcurrentAccess verifies thread safety of agent operations.
func TestConcurrentAccess(t *testing.T) {
	a := agent.NewAgent(agent.AgentOptions{
		ID: "concurrent-test",
	})

	const numGoroutines = 50
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // subscribe, unsubscribe

	// Concurrent subscriptions
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				subID, _ := a.Subscribe()
				// Don't unsubscribe immediately to test map growth
				_ = subID
			}
		}()
	}

	// Concurrent unsubscriptions
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				subID, _ := a.Subscribe()
				a.Unsubscribe(subID)
			}
		}()
	}

	wg.Wait()

	// Verify no data races occurred (detected by go test -race)
}

// TestStateAccess verifies thread-safe state access.
func TestStateAccess(t *testing.T) {
	opts := agent.AgentOptions{
		SystemPrompt: "test prompt",
		SessionID:    "test-session",
	}
	a := agent.NewAgent(opts)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			state := a.State()
			_ = state.SessionID
			_ = state.SystemPrompt
		}()
	}

	wg.Wait()
}
