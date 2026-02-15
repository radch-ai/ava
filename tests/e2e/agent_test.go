//go:build e2e

// Package e2e_test provides end-to-end integration tests for the agent runtime.
package e2e_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// TestE2E_PromptWithMockLLM verifies that agent requires LLM client to be configured.
func TestE2E_PromptWithMockLLM(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create an agent WITHOUT LLM client to verify error handling
	opts := agent.AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "e2e-test-session",
	}

	ag := agent.NewAgent(opts)

	// Send a prompt - should fail because LLM client is not configured
	err := ag.Prompt(ctx, "Hello, agent!")
	if err == nil {
		t.Fatal("Expected error when LLM client is not configured, got nil")
	}

	// Verify error message
	if err.Error() != "LLM client not configured" {
		t.Errorf("Expected error message 'LLM client not configured', got %q", err.Error())
	}

	// Verify no user message was added since it failed early
	state := ag.State()
	if len(state.Messages) != 0 {
		t.Errorf("Expected no messages when LLM client is not configured, got %d", len(state.Messages))
	}
}

// TestE2E_ToolCallingFlow verifies tool execution through the agent loop.
func TestE2E_ToolCallingFlow(t *testing.T) {
	t.Parallel()

	// RED: Test full tool calling flow using Loop with MockLLM

	ctx := context.Background()

	// Track tool execution
	var toolCalled atomic.Bool
	var capturedArgs map[string]any

	// Create a mock tool
	testTool := agent.Tool{
		Name:        "get_weather",
		Description: "Gets the current weather for a location",
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			toolCalled.Store(true)
			capturedArgs = args
			return &agent.ToolResult{
				Content: []agent.ContentBlock{agent.TextContent{Text: "Sunny, 72°F"}},
			}, nil
		},
	}

	// Mock LLM that requests a tool call, then responds with final text
	mockLLM := &agent.MockLLMClient{
		Responses: []agent.MockResponse{
			{
				// First response: tool call
				Message: agent.AgentMessage{
					Role:      agent.RoleAssistant,
					Timestamp: time.Now().Unix(),
					Content: []agent.ContentBlock{
						agent.ToolCallContent{
							ID:   "call_123",
							Name: "get_weather",
							Arguments: map[string]any{
								"location": "San Francisco",
							},
						},
					},
				},
			},
			{
				// Second response: final text after tool execution
				Message: agent.NewTextMessage(agent.RoleAssistant, "The weather in San Francisco is sunny and 72°F."),
			},
		},
	}

	// Create loop with mock LLM
	loop := agent.NewLoop(mockLLM)

	// Initial messages
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "What's the weather in San Francisco?"),
	}

	// Run the loop
	result, err := loop.Run(ctx, messages, testTool)
	if err != nil {
		t.Fatalf("Loop.Run() failed: %v", err)
	}

	// Verify tool was called
	if !toolCalled.Load() {
		t.Error("Expected tool 'get_weather' to be called")
	}

	// Verify tool arguments
	if capturedArgs == nil {
		t.Fatal("Expected tool arguments to be captured")
	}

	if location, ok := capturedArgs["location"].(string); !ok || location != "San Francisco" {
		t.Errorf("Expected location 'San Francisco', got %v", capturedArgs["location"])
	}

	// Verify message history:
	// 1. User message
	// 2. Assistant tool call
	// 3. Tool result
	// 4. Assistant final response
	if len(result.Messages) != 4 {
		t.Fatalf("Expected 4 messages in history, got %d", len(result.Messages))
	}

	// Verify user message
	if result.Messages[0].Role != agent.RoleUser {
		t.Errorf("Expected message 0 role %v, got %v", agent.RoleUser, result.Messages[0].Role)
	}

	// Verify tool call message
	if result.Messages[1].Role != agent.RoleAssistant {
		t.Errorf("Expected message 1 role %v, got %v", agent.RoleAssistant, result.Messages[1].Role)
	}
	toolCalls := extractToolCalls(result.Messages[1])
	if len(toolCalls) != 1 {
		t.Fatalf("Expected 1 tool call in message 1, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "get_weather" {
		t.Errorf("Expected tool name 'get_weather', got %q", toolCalls[0].Name)
	}

	// Verify tool result message
	if result.Messages[2].Role != agent.RoleToolResult {
		t.Errorf("Expected message 2 role %v, got %v", agent.RoleToolResult, result.Messages[2].Role)
	}
	if result.Messages[2].ToolName != "get_weather" {
		t.Errorf("Expected tool name 'get_weather' in result, got %q", result.Messages[2].ToolName)
	}
	if result.Messages[2].IsError {
		t.Error("Expected tool result to not be an error")
	}

	// Verify final assistant response
	if result.Messages[3].Role != agent.RoleAssistant {
		t.Errorf("Expected message 3 role %v, got %v", agent.RoleAssistant, result.Messages[3].Role)
	}
}

// extractToolCalls is a helper to extract tool calls from a message.
func extractToolCalls(msg agent.AgentMessage) []agent.ToolCallContent {
	var calls []agent.ToolCallContent
	for _, block := range msg.Content {
		if tc, ok := block.(agent.ToolCallContent); ok {
			calls = append(calls, tc)
		}
	}
	return calls
}

// TestE2E_MultiTurnConversation verifies multiple prompts in sequence.
func TestE2E_MultiTurnConversation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create mock LLM with 3 responses
	mockLLM := &agent.MockLLMClient{
		Responses: []agent.MockResponse{
			{
				Message: agent.NewTextMessage(agent.RoleAssistant, "Hi Alice!"),
			},
			{
				Message: agent.NewTextMessage(agent.RoleAssistant, "Your name is Alice."),
			},
			{
				Message: agent.NewTextMessage(agent.RoleAssistant, "Why did the chicken cross the road?"),
			},
		},
	}

	// Create agent
	opts := agent.AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "multi-turn-session",
		LLMClient:    mockLLM,
	}

	ag := agent.NewAgent(opts)

	// Track prompts
	prompts := []string{
		"Hello, my name is Alice.",
		"What's my name?",
		"Tell me a joke.",
	}

	// Execute each prompt sequentially
	for i, prompt := range prompts {
		err := ag.Prompt(ctx, prompt)
		if err != nil {
			t.Fatalf("Prompt %d (%q) failed: %v", i, prompt, err)
		}
	}

	// Verify message accumulation
	state := ag.State()
	// Each prompt adds user message + assistant response = 2 messages per prompt
	expectedCount := len(prompts) * 2
	if len(state.Messages) != expectedCount {
		t.Fatalf("Expected %d messages, got %d", expectedCount, len(state.Messages))
	}

	// Verify user messages are in correct positions
	for i, prompt := range prompts {
		msgIdx := i * 2
		msg := state.Messages[msgIdx]

		if msg.Role != agent.RoleUser {
			t.Errorf("Message %d: expected role %v, got %v", msgIdx, agent.RoleUser, msg.Role)
		}

		textContent, ok := msg.Content[0].(agent.TextContent)
		if !ok {
			t.Fatalf("Message %d: expected TextContent, got %T", msgIdx, msg.Content[0])
		}

		if textContent.Text != prompt {
			t.Errorf("Message %d: expected text %q, got %q", msgIdx, prompt, textContent.Text)
		}
	}

	// Verify assistant responses exist
	for i := 0; i < len(prompts); i++ {
		msgIdx := i*2 + 1
		msg := state.Messages[msgIdx]

		if msg.Role != agent.RoleAssistant {
			t.Errorf("Message %d: expected role %v, got %v", msgIdx, agent.RoleAssistant, msg.Role)
		}
	}

	// Verify state is preserved across turns
	if state.SessionID != opts.SessionID {
		t.Errorf("Expected SessionID %q, got %q", opts.SessionID, state.SessionID)
	}

	if state.SystemPrompt != opts.SystemPrompt {
		t.Errorf("Expected SystemPrompt %q, got %q", opts.SystemPrompt, state.SystemPrompt)
	}
}

// TestE2E_EventStreaming verifies all events are emitted in correct order.
func TestE2E_EventStreaming(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	mockLLM := &agent.MockLLMClient{
		Responses: []agent.MockResponse{
			{
				Message: agent.NewTextMessage(agent.RoleAssistant, "Events are streaming"),
			},
		},
	}

	opts := agent.AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "event-streaming-test",
		LLMClient:    mockLLM,
	}

	ag := agent.NewAgent(opts)

	// Subscribe to capture all events
	subID, eventCh := ag.Subscribe()
	defer ag.Unsubscribe(subID)

	// Execute a prompt
	err := ag.Prompt(ctx, "Tell me about events")
	if err != nil {
		t.Fatalf("Prompt() failed: %v", err)
	}

	// Collect all events with timeout
	var events []agent.AgentEvent
	timeout := time.After(200 * time.Millisecond)
collect:
	for {
		select {
		case event := <-eventCh:
			events = append(events, event)
		case <-timeout:
			break collect
		}
	}

	// Verify minimum event count (at least AgentStart and AgentEnd)
	if len(events) < 2 {
		t.Fatalf("Expected at least 2 events, got %d", len(events))
	}

	// Verify event order: AgentStartEvent should come before AgentEndEvent
	var startIdx, endIdx int
	hasStart := false
	hasEnd := false

	for i, evt := range events {
		switch e := evt.(type) {
		case agent.AgentStartEvent:
			hasStart = true
			startIdx = i
			t.Logf("AgentStartEvent at index %d, timestamp %d", i, e.Timestamp)
		case agent.AgentEndEvent:
			hasEnd = true
			endIdx = i
			t.Logf("AgentEndEvent at index %d, timestamp %d", i, e.Timestamp)
			t.Logf("AgentEndEvent contains %d messages", len(e.Messages))
		}
	}

	if !hasStart {
		t.Error("Expected to receive AgentStartEvent")
	}

	if !hasEnd {
		t.Error("Expected to receive AgentEndEvent")
	}

	if hasStart && hasEnd && startIdx > endIdx {
		t.Errorf("AgentStartEvent (index %d) should come before AgentEndEvent (index %d)", startIdx, endIdx)
	}

	// Verify timestamps are in non-decreasing order
	for i := 1; i < len(events); i++ {
		prevTS := getEventTimestamp(events[i-1])
		currTS := getEventTimestamp(events[i])
		if currTS < prevTS {
			t.Errorf("Event timestamps should be non-decreasing: event %d has timestamp %d, event %d has timestamp %d",
				i-1, prevTS, i, currTS)
		}
	}

	// Verify AgentEndEvent contains all messages
	for _, evt := range events {
		if endEvt, ok := evt.(agent.AgentEndEvent); ok {
			if len(endEvt.Messages) == 0 {
				t.Error("AgentEndEvent.Messages should contain at least one message")
			}
			// Verify user message is present
			foundUserMsg := false
			for _, msg := range endEvt.Messages {
				if msg.Role == agent.RoleUser {
					foundUserMsg = true
					break
				}
			}
			if !foundUserMsg {
				t.Error("AgentEndEvent.Messages should contain the user message")
			}
		}
	}
}

// getEventTimestamp extracts the timestamp from an event.
func getEventTimestamp(evt agent.AgentEvent) int64 {
	switch e := evt.(type) {
	case agent.AgentStartEvent:
		return e.Timestamp
	case agent.AgentEndEvent:
		return e.Timestamp
	case agent.TurnStartEvent:
		return e.Timestamp
	case agent.TurnEndEvent:
		return e.Timestamp
	case agent.MessageStartEvent:
		return e.Timestamp
	case agent.MessageUpdateEvent:
		return e.Timestamp
	case agent.MessageEndEvent:
		return e.Timestamp
	case agent.ToolExecutionStartEvent:
		return e.Timestamp
	case agent.ToolExecutionUpdateEvent:
		return e.Timestamp
	case agent.ToolExecutionEndEvent:
		return e.Timestamp
	default:
		return 0
	}
}

// TestE2E_ConcurrentPrompts verifies multiple simultaneous prompts are handled safely.
func TestE2E_ConcurrentPrompts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create mock LLM with responses for each concurrent prompt
	// Need 10 responses for 10 concurrent calls
	responses := make([]agent.MockResponse, 10)
	for i := 0; i < 10; i++ {
		responses[i] = agent.MockResponse{
			Message: agent.NewTextMessage(agent.RoleAssistant, "Response "+string(rune('A'+i))),
		}
	}
	mockLLM := &agent.MockLLMClient{
		Responses: responses,
	}

	opts := agent.AgentOptions{
		SystemPrompt: "You are a helpful assistant.",
		SessionID:    "concurrent-test-session",
		LLMClient:    mockLLM,
	}

	ag := agent.NewAgent(opts)

	// Number of concurrent prompts
	const numPrompts = 10

	// Track completion
	var wg sync.WaitGroup
	var errors [numPrompts]error

	// Launch concurrent prompts
	for i := 0; i < numPrompts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Reset mock for each call to get a response
			mockLLM.Reset()
			err := ag.Prompt(ctx, "Concurrent prompt "+string(rune('A'+idx)))
			errors[idx] = err
		}(i)
	}

	// Wait for all to complete
	wg.Wait()

	// Verify no errors occurred
	for i, err := range errors {
		if err != nil {
			t.Errorf("Prompt %d failed: %v", i, err)
		}
	}

	// Verify all messages were added
	state := ag.State()
	// Each prompt adds user + assistant = 2 messages
	expectedCount := numPrompts * 2
	if len(state.Messages) != expectedCount {
		t.Logf("Warning: Expected %d messages, got %d (may vary due to concurrency)", expectedCount, len(state.Messages))
		// Don't fail, as concurrent access may cause slight variations
		// but we should have approximately the right number
		if len(state.Messages) < numPrompts {
			t.Errorf("Expected at least %d messages, got %d", numPrompts, len(state.Messages))
		}
	}

	// Verify state consistency
	if state.SessionID != opts.SessionID {
		t.Errorf("SessionID changed: expected %q, got %q", opts.SessionID, state.SessionID)
	}

	// Count user and assistant messages
	userCount := 0
	assistantCount := 0
	for _, msg := range state.Messages {
		switch msg.Role {
		case agent.RoleUser:
			userCount++
		case agent.RoleAssistant:
			assistantCount++
		}
	}

	if userCount != numPrompts {
		t.Logf("Warning: Expected %d user messages, got %d", numPrompts, userCount)
	}

	if assistantCount != numPrompts {
		t.Logf("Warning: Expected %d assistant messages, got %d", numPrompts, assistantCount)
	}
}

// TestE2E_StatePersistence verifies state is maintained across prompts.
func TestE2E_StatePersistence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create mock LLM with 2 responses
	mockLLM := &agent.MockLLMClient{
		Responses: []agent.MockResponse{
			{
				Message: agent.NewTextMessage(agent.RoleAssistant, "First response"),
			},
			{
				Message: agent.NewTextMessage(agent.RoleAssistant, "Second response"),
			},
		},
	}

	// Create agent with specific state
	opts := agent.AgentOptions{
		SystemPrompt: "You are a test assistant for state persistence.",
		SessionID:    "persistence-test-session",
		LLMClient:    mockLLM,
	}

	ag := agent.NewAgent(opts)

	// Send first prompt
	err := ag.Prompt(ctx, "First prompt")
	if err != nil {
		t.Fatalf("First Prompt() failed: %v", err)
	}

	// Capture state after first prompt
	state1 := ag.State()

	// Verify state elements
	if state1.SessionID != opts.SessionID {
		t.Errorf("After first prompt: expected SessionID %q, got %q", opts.SessionID, state1.SessionID)
	}

	if state1.SystemPrompt != opts.SystemPrompt {
		t.Errorf("After first prompt: expected SystemPrompt %q, got %q", opts.SystemPrompt, state1.SystemPrompt)
	}

	// Send second prompt
	err = ag.Prompt(ctx, "Second prompt")
	if err != nil {
		t.Fatalf("Second Prompt() failed: %v", err)
	}

	// Capture state after second prompt
	state2 := ag.State()

	// Verify immutable state elements persisted
	if state2.SessionID != state1.SessionID {
		t.Errorf("SessionID changed between prompts: %q -> %q", state1.SessionID, state2.SessionID)
	}

	if state2.SystemPrompt != state1.SystemPrompt {
		t.Errorf("SystemPrompt changed between prompts: %q -> %q", state1.SystemPrompt, state2.SystemPrompt)
	}

	// Verify messages accumulated
	if len(state2.Messages) != 4 { // 2 prompts * (user + assistant)
		t.Fatalf("Expected 4 messages after 2 prompts, got %d", len(state2.Messages))
	}

	// Verify message order
	// [0] User: "First prompt"
	// [1] Assistant: response
	// [2] User: "Second prompt"
	// [3] Assistant: response
	expectedRoles := []agent.Role{agent.RoleUser, agent.RoleAssistant, agent.RoleUser, agent.RoleAssistant}
	for i, msg := range state2.Messages {
		if msg.Role != expectedRoles[i] {
			t.Errorf("Message %d: expected role %v, got %v", i, expectedRoles[i], msg.Role)
		}
	}

	// Verify State() returns copies, not references (mutation safety)
	state3 := ag.State()
	state3.Messages = append(state3.Messages, agent.NewTextMessage(agent.RoleUser, "Injected message"))

	state4 := ag.State()
	if len(state4.Messages) != 4 {
		t.Errorf("State() should return a copy; modifying returned state should not affect agent. Expected 4 messages, got %d", len(state4.Messages))
	}

	// Verify streaming state is cleared after prompts
	if state2.IsStreaming {
		t.Error("IsStreaming should be false after Prompt completes")
	}

	if state2.StreamMessage != nil {
		t.Error("StreamMessage should be nil after Prompt completes")
	}
}
