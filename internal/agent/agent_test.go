// Package agent provides core types and events for the agent runtime.
package agent

import (
	"sync"
	"testing"
	"time"
)

// TestNewAgent verifies agent initialization with default values.
func TestNewAgent(t *testing.T) {
	opts := AgentOptions{
		ID:           "test-agent-1",
		SystemPrompt: "You are a helpful assistant",
		SessionID:    "session-123",
	}

	agent := NewAgent(opts)

	if agent == nil {
		t.Fatal("NewAgent returned nil")
	}

	agent.mu.RLock()
	defer agent.mu.RUnlock()

	if agent.id != opts.ID {
		t.Errorf("expected ID %q, got %q", opts.ID, agent.id)
	}

	if agent.state.SessionID != opts.SessionID {
		t.Errorf("expected SessionID %q, got %q", opts.SessionID, agent.state.SessionID)
	}

	if agent.state.SystemPrompt != opts.SystemPrompt {
		t.Errorf("expected SystemPrompt %q, got %q", opts.SystemPrompt, agent.state.SystemPrompt)
	}

	if agent.ctx == nil {
		t.Error("context should not be nil")
	}

	if agent.cancel == nil {
		t.Error("cancel function should not be nil")
	}
}

// TestNewAgentDefaults verifies agent initialization with minimal options.
func TestNewAgentDefaults(t *testing.T) {
	opts := AgentOptions{}

	agent := NewAgent(opts)

	agent.mu.RLock()
	defer agent.mu.RUnlock()

	if agent.id == "" {
		t.Error("ID should be generated if not provided")
	}

	if agent.subscribers == nil {
		t.Error("subscribers map should be initialized")
	}
}

// TestSubscribeUnsubscribe verifies event subscription lifecycle.
func TestSubscribeUnsubscribe(t *testing.T) {
	agent := NewAgent(AgentOptions{})

	// Subscribe
	subID, ch := agent.Subscribe()

	if subID == "" {
		t.Fatal("subscription ID should not be empty")
	}

	if ch == nil {
		t.Fatal("channel should not be nil")
	}

	// Verify subscriber is registered
	agent.subscribersMu.Lock()
	if _, exists := agent.subscribers[subID]; !exists {
		t.Error("subscriber not registered in map")
	}
	agent.subscribersMu.Unlock()

	// Unsubscribe
	agent.Unsubscribe(subID)

	// Verify subscriber is removed
	agent.subscribersMu.Lock()
	if _, exists := agent.subscribers[subID]; exists {
		t.Error("subscriber still registered after unsubscribe")
	}
	agent.subscribersMu.Unlock()
}

// TestEmit verifies event broadcasting to subscribers.
func TestEmit(t *testing.T) {
	agent := NewAgent(AgentOptions{})

	// Subscribe multiple listeners
	subID1, _ := agent.Subscribe()
	subID2, _ := agent.Subscribe()

	// Buffer channels to receive events
	ch1 := make(chan AgentEvent, 10)
	ch2 := make(chan AgentEvent, 10)

	agent.subscribersMu.Lock()
	agent.subscribers[subID1] = ch1
	agent.subscribers[subID2] = ch2
	agent.subscribersMu.Unlock()

	// Emit an event
	event := TurnStartEvent{Timestamp: time.Now().Unix()}
	agent.emit(event)

	// Verify both subscribers received the event
	select {
	case received := <-ch1:
		if _, ok := received.(TurnStartEvent); !ok {
			t.Errorf("expected TurnStartEvent, got %T", received)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber 1 did not receive event")
	}

	select {
	case received := <-ch2:
		if _, ok := received.(TurnStartEvent); !ok {
			t.Errorf("expected TurnStartEvent, got %T", received)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber 2 did not receive event")
	}

	// Cleanup
	agent.Unsubscribe(subID1)
	agent.Unsubscribe(subID2)
}

// TestConcurrentAccess verifies thread safety of agent operations.
func TestConcurrentAccess(t *testing.T) {
	agent := NewAgent(AgentOptions{
		ID: "concurrent-test",
	})

	const numGoroutines = 50
	const numOps = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 3) // subscribe, unsubscribe, emit

	// Concurrent subscriptions
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				subID, _ := agent.Subscribe()
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
				subID, _ := agent.Subscribe()
				agent.Unsubscribe(subID)
			}
		}()
	}

	// Concurrent emits
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				event := TurnStartEvent{Timestamp: time.Now().Unix()}
				agent.emit(event)
			}
		}()
	}

	wg.Wait()

	// Verify no data races occurred (detected by go test -race)
	// If we get here without panic/deadlock, test passes
}

// TestAgentContext verifies context cancellation propagates.
func TestAgentContext(t *testing.T) {
	agent := NewAgent(AgentOptions{})

	// Context should not be cancelled initially
	select {
	case <-agent.ctx.Done():
		t.Fatal("context should not be cancelled initially")
	default:
	}

	// Cancel the context
	agent.cancel()

	// Context should now be cancelled
	select {
	case <-agent.ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context should be cancelled after calling cancel()")
	}
}

// TestStateAccess verifies thread-safe state access.
func TestStateAccess(t *testing.T) {
	opts := AgentOptions{
		SystemPrompt: "test prompt",
		SessionID:    "test-session",
	}
	agent := NewAgent(opts)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			agent.mu.RLock()
			_ = agent.state.SessionID
			_ = agent.state.SystemPrompt
			agent.mu.RUnlock()
		}()
	}

	wg.Wait()
}
