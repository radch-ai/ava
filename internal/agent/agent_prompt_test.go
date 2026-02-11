// Package agent provides core types and tests for the agent runtime framework.
package agent

import (
	"context"
	"testing"
	"time"
)

// TestAgent_PromptAddsUserMessage verifies that calling Prompt adds the user message to state.Messages.
func TestAgent_PromptAddsUserMessage(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)
	ctx := context.Background()

	err := a.Prompt(ctx, "Hello, agent!")
	if err != nil {
		t.Fatalf("Prompt() failed: %v", err)
	}

	state := a.State()
	if len(state.Messages) == 0 {
		t.Fatal("Expected at least 1 message in state.Messages, got 0")
	}

	msg := state.Messages[0]
	if msg.Role != RoleUser {
		t.Errorf("Expected message role %v, got %v", RoleUser, msg.Role)
	}

	if len(msg.Content) == 0 {
		t.Fatal("Expected message to have content blocks, got 0")
	}

	textContent, ok := msg.Content[0].(TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", msg.Content[0])
	}

	if textContent.Text != "Hello, agent!" {
		t.Errorf("Expected text 'Hello, agent!', got %q", textContent.Text)
	}
}

// TestAgent_PromptEmitsAgentStartEvent verifies that Prompt emits AgentStartEvent.
func TestAgent_PromptEmitsAgentStartEvent(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)

	// Subscribe to events
	subID, ch := a.Subscribe()
	defer a.Unsubscribe(subID)

	ctx := context.Background()
	err := a.Prompt(ctx, "Hello")
	if err != nil {
		t.Fatalf("Prompt() failed: %v", err)
	}

	// Drain channel looking for AgentStartEvent
	found := false
	timeout := time.After(100 * time.Millisecond)
	for !found {
		select {
		case event := <-ch:
			if _, ok := event.(AgentStartEvent); ok {
				found = true
			}
		case <-timeout:
			t.Fatal("Timed out waiting for AgentStartEvent")
		}
		if found {
			break
		}
	}

	if !found {
		t.Error("Expected AgentStartEvent to be emitted, but it was not")
	}
}

// TestAgent_PromptEmitsAgentEndEvent verifies that Prompt emits AgentEndEvent.
func TestAgent_PromptEmitsAgentEndEvent(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)

	// Subscribe to events
	subID, ch := a.Subscribe()
	defer a.Unsubscribe(subID)

	ctx := context.Background()
	err := a.Prompt(ctx, "Hello")
	if err != nil {
		t.Fatalf("Prompt() failed: %v", err)
	}

	// Drain channel looking for AgentEndEvent
	found := false
	var endMessages []AgentMessage
	timeout := time.After(100 * time.Millisecond)
	for !found {
		select {
		case event := <-ch:
			if evt, ok := event.(AgentEndEvent); ok {
				found = true
				endMessages = evt.Messages
			}
		case <-timeout:
			t.Fatal("Timed out waiting for AgentEndEvent")
		}
		if found {
			break
		}
	}

	if !found {
		t.Error("Expected AgentEndEvent to be emitted, but it was not")
	}

	if len(endMessages) == 0 {
		t.Error("Expected AgentEndEvent.Messages to contain at least the user message, got empty slice")
	}
}

// TestAgent_PromptSetsStreamingState verifies that Prompt sets and clears streaming state.
func TestAgent_PromptSetsStreamingState(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)

	ctx := context.Background()
	err := a.Prompt(ctx, "Hello")
	if err != nil {
		t.Fatalf("Prompt() failed: %v", err)
	}

	// After completion, streaming should be false
	state := a.State()
	if state.IsStreaming {
		t.Error("Expected IsStreaming to be false after Prompt completes, got true")
	}

	if state.StreamMessage != nil {
		t.Error("Expected StreamMessage to be nil after Prompt completes")
	}
}

// TestAgent_PromptContextCancellation verifies that Prompt respects context cancellation.
func TestAgent_PromptContextCancellation(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := a.Prompt(ctx, "Hello")
	if err == nil {
		t.Error("Expected error when context is already cancelled, got nil")
	}

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}
}

// TestAgent_PromptConcurrent verifies that concurrent Prompt calls are handled safely.
func TestAgent_PromptConcurrent(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)

	// Launch multiple concurrent prompts
	const numPrompts = 5
	errs := make(chan error, numPrompts)

	for i := 0; i < numPrompts; i++ {
		go func(idx int) {
			ctx := context.Background()
			errs <- a.Prompt(ctx, "Concurrent message")
		}(i)
	}

	// Collect results
	for i := 0; i < numPrompts; i++ {
		if err := <-errs; err != nil {
			t.Errorf("Prompt %d failed: %v", i, err)
		}
	}

	state := a.State()
	// Each prompt adds a user message and a simulated assistant response
	expectedMessages := numPrompts * 2
	if len(state.Messages) != expectedMessages {
		t.Errorf("Expected %d messages after concurrent prompts, got %d", expectedMessages, len(state.Messages))
	}
}

// TestAgent_StateThreadSafety verifies that State() can be called concurrently.
func TestAgent_StateThreadSafety(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)

	// Call State() concurrently
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = a.State()
		}
		close(done)
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = a.State()
		}
	}()

	<-done
	// If we get here without race detector complaining, we're good
}

// TestAgent_SubscribeUnsubscribe verifies event subscription and unsubscription.
func TestAgent_SubscribeUnsubscribe(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)

	subID1, ch1 := a.Subscribe()

	ctx := context.Background()
	err := a.Prompt(ctx, "Hello")
	if err != nil {
		t.Fatalf("Prompt() failed: %v", err)
	}

	// Should have received events
	eventCount := 0
drainLoop:
	for {
		select {
		case <-ch1:
			eventCount++
		case <-time.After(50 * time.Millisecond):
			break drainLoop
		}
	}
	if eventCount == 0 {
		t.Error("Expected to receive at least one event")
	}

	// Unsubscribe and verify channel is closed
	a.Unsubscribe(subID1)

	// Drain any remaining buffered events, then check for close
drainClose:
	for {
		select {
		case _, ok := <-ch1:
			if !ok {
				// Channel is closed as expected
				break drainClose
			}
			// Still draining buffered events
		case <-time.After(100 * time.Millisecond):
			// No more events and no close signal
			break drainClose
		}
	}

	// Verify channel is truly closed by attempting another read
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("Expected channel to be closed after unsubscribe and draining")
		}
		// Channel is closed
	case <-time.After(10 * time.Millisecond):
		// No event received, channel is likely closed
	}

	// Create new subscription for second prompt
	subID2, ch2 := a.Subscribe()
	defer a.Unsubscribe(subID2)

	initialCount := getEventCount(ch2)
	err = a.Prompt(ctx, "Hello again")
	if err != nil {
		t.Fatalf("Second Prompt() failed: %v", err)
	}

	newCount := getEventCount(ch2)
	if newCount == initialCount {
		t.Error("Expected new events after second Prompt, but received none")
	}
}

// getEventCount drains and counts events from a channel within a timeout.
func getEventCount(ch <-chan AgentEvent) int {
	count := 0
	timeout := time.After(50 * time.Millisecond)
drain:
	for {
		select {
		case <-ch:
			count++
		case <-timeout:
			break drain
		}
	}
	return count
}

// TestAgent_NewAgent verifies agent initialization.
func TestAgent_NewAgent(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "session-abc",
	}

	a := NewAgent(opts)
	if a == nil {
		t.Fatal("NewAgent() returned nil")
	}

	state := a.State()
	if state.SystemPrompt != opts.SystemPrompt {
		t.Errorf("Expected SystemPrompt %q, got %q", opts.SystemPrompt, state.SystemPrompt)
	}

	if state.SessionID != opts.SessionID {
		t.Errorf("Expected SessionID %q, got %q", opts.SessionID, state.SessionID)
	}

	if len(state.Messages) != 0 {
		t.Errorf("Expected empty Messages slice, got %d messages", len(state.Messages))
	}

	if state.IsStreaming {
		t.Error("Expected IsStreaming to be false initially")
	}
}

// TestAgent_PromptAddsMultipleMessages verifies multiple sequential prompts.
func TestAgent_PromptAddsMultipleMessages(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)
	ctx := context.Background()

	messages := []string{"First message", "Second message", "Third message"}
	for _, msg := range messages {
		err := a.Prompt(ctx, msg)
		if err != nil {
			t.Fatalf("Prompt() failed for message %q: %v", msg, err)
		}
	}

	state := a.State()
	// Each prompt adds user message + assistant response
	expectedCount := len(messages) * 2
	if len(state.Messages) != expectedCount {
		t.Fatalf("Expected %d messages, got %d", expectedCount, len(state.Messages))
	}

	// Verify user messages are in the right positions (even indices)
	for i, expectedMsg := range messages {
		msg := state.Messages[i*2] // User messages are at 0, 2, 4
		if msg.Role != RoleUser {
			t.Errorf("Message %d: expected role %v, got %v", i*2, RoleUser, msg.Role)
		}

		textContent, ok := msg.Content[0].(TextContent)
		if !ok {
			t.Fatalf("Message %d: expected TextContent, got %T", i*2, msg.Content[0])
		}

		if textContent.Text != expectedMsg {
			t.Errorf("Message %d: expected text %q, got %q", i*2, expectedMsg, textContent.Text)
		}
	}

	// Verify assistant responses exist at odd indices
	for i := 0; i < len(messages); i++ {
		msg := state.Messages[i*2+1]
		if msg.Role != RoleAssistant {
			t.Errorf("Message %d: expected role %v, got %v", i*2+1, RoleAssistant, msg.Role)
		}
	}
}

// TestAgent_PromptEventOrder verifies that events are emitted in correct order.
func TestAgent_PromptEventOrder(t *testing.T) {
	t.Parallel()

	opts := AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "test-session",
	}

	a := NewAgent(opts)

	subID, ch := a.Subscribe()
	defer a.Unsubscribe(subID)

	ctx := context.Background()
	err := a.Prompt(ctx, "Test event order")
	if err != nil {
		t.Fatalf("Prompt() failed: %v", err)
	}

	// Collect all events
	var events []AgentEvent
	timeout := time.After(100 * time.Millisecond)
collect:
	for {
		select {
		case event := <-ch:
			events = append(events, event)
		case <-timeout:
			break collect
		}
	}

	// Verify we have AgentStartEvent before AgentEndEvent
	var startIndex, endIndex int
	hasStart := false
	hasEnd := false

	for i, evt := range events {
		switch evt.(type) {
		case AgentStartEvent:
			startIndex = i
			hasStart = true
		case AgentEndEvent:
			endIndex = i
			hasEnd = true
		}
	}

	if !hasStart {
		t.Error("Expected to find AgentStartEvent")
	}

	if !hasEnd {
		t.Error("Expected to find AgentEndEvent")
	}

	if hasStart && hasEnd && startIndex > endIndex {
		t.Error("AgentStartEvent should come before AgentEndEvent")
	}
}
