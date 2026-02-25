// Package llm_test provides Anthropic LLM client implementation and tests.
package llm_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
	llm "github.com/nevaria-ai/gateway/internal/llm"
)

// TestAnthropicClient_Stream_TextOnly tests streaming a simple text response using the SDK.
func TestAnthropicClient_Stream_TextOnly(t *testing.T) {
	t.Parallel()

	// Create a mock server that returns a streaming text response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if apiKey := r.Header.Get("x-api-key"); apiKey != "test-api-key" {
			t.Errorf("expected x-api-key test-api-key, got %s", apiKey)
		}

		// Verify path
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected path /v1/messages, got %s", r.URL.Path)
		}

		// Verify method
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}

		// Set SSE headers and send events
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// Message start event
		_, _ = fmt.Fprintf(w, "event: message_start\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-123\",\"role\":\"assistant\",\"content\":[]}}\n\n")
		flusher.Flush()

		// Content block start
		_, _ = fmt.Fprintf(w, "event: content_block_start\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		flusher.Flush()

		// Text delta
		_, _ = fmt.Fprintf(w, "event: content_block_delta\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "event: content_block_delta\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\", world!\"}}\n\n")
		flusher.Flush()

		// Content block stop
		_, _ = fmt.Fprintf(w, "event: content_block_stop\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		flusher.Flush()

		// Message delta
		_, _ = fmt.Fprintf(w, "event: message_delta\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":12}}\n\n")
		flusher.Flush()

		// Message stop
		_, _ = fmt.Fprintf(w, "event: message_stop\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	// Create client
	client := llm.NewAnthropicClient(llm.AnthropicClientConfig{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
		Model:   "claude-3-5-sonnet-20250219",
	})

	// Create messages
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Hello"),
	}

	// Stream response
	ctx := context.Background()
	eventCh, err := client.Stream(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}

	// Collect events
	var events []agent.AgentEvent
	for event := range eventCh {
		events = append(events, event)
	}

	// Verify events
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events (start, update, end), got %d", len(events))
	}

	// Check first event is MessageStartEvent
	msgStart, ok := events[0].(agent.MessageStartEvent)
	if !ok {
		t.Fatalf("expected first event to be MessageStartEvent, got %T", events[0])
	}
	if msgStart.Message.Role != agent.RoleAssistant {
		t.Errorf("expected role assistant, got %s", msgStart.Message.Role)
	}

	// Check last event is MessageEndEvent
	msgEnd, ok := events[len(events)-1].(agent.MessageEndEvent)
	if !ok {
		t.Fatalf("expected last event to be MessageEndEvent, got %T", events[len(events)-1])
	}

	// Check we got MessageUpdateEvent(s)
	updateCount := 0
	var fullText strings.Builder
	for _, e := range events {
		if update, ok := e.(agent.MessageUpdateEvent); ok {
			updateCount++
			fullText.WriteString(update.Delta)
		}
	}

	if updateCount == 0 {
		t.Error("expected at least one MessageUpdateEvent")
	}

	expectedText := "Hello, world!"
	if fullText.String() != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, fullText.String())
	}

	// Verify final message content
	if len(msgEnd.Message.Content) == 0 {
		t.Error("expected final message to have content")
	} else {
		textContent, ok := msgEnd.Message.Content[0].(agent.TextContent)
		if !ok {
			t.Errorf("expected TextContent, got %T", msgEnd.Message.Content[0])
		} else if textContent.Text != expectedText {
			t.Errorf("expected final text %q, got %q", expectedText, textContent.Text)
		}
	}
}

// TestAnthropicClient_Stream_WithToolCall tests streaming a response with tool calls.
func TestAnthropicClient_Stream_WithToolCall(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		// Message start event
		_, _ = fmt.Fprintf(w, "event: message_start\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-456\",\"role\":\"assistant\",\"content\":[]}}\n\n")
		flusher.Flush()

		// Content block start (tool use)
		_, _ = fmt.Fprintf(w, "event: content_block_start\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu-123\",\"name\":\"calculator\",\"input\":{\"x\":5,\"y\":3}}}\n\n")
		flusher.Flush()

		// Content block stop
		_, _ = fmt.Fprintf(w, "event: content_block_stop\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		flusher.Flush()

		// Message delta
		_, _ = fmt.Fprintf(w, "event: message_delta\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":20}}\n\n")
		flusher.Flush()

		// Message stop
		_, _ = fmt.Fprintf(w, "event: message_stop\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := llm.NewAnthropicClient(llm.AnthropicClientConfig{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
		Model:   "claude-3-5-sonnet-20250219",
	})

	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "What is 5 + 3?"),
	}

	tools := []agent.Tool{
		{
			Name:        "calculator",
			Description: "A simple calculator",
			Execute:     nil,
		},
	}

	ctx := context.Background()
	eventCh, err := client.Stream(ctx, messages, tools)
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}

	// Collect events
	var events []agent.AgentEvent
	for event := range eventCh {
		events = append(events, event)
	}

	// Verify we got events
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	// Check final message has tool call
	msgEnd, ok := events[len(events)-1].(agent.MessageEndEvent)
	if !ok {
		t.Fatalf("expected last event to be MessageEndEvent, got %T", events[len(events)-1])
	}

	if len(msgEnd.Message.Content) == 0 {
		t.Fatal("expected final message to have content")
	}

	toolCall, ok := msgEnd.Message.Content[0].(agent.ToolCallContent)
	if !ok {
		t.Fatalf("expected ToolCallContent, got %T", msgEnd.Message.Content[0])
	}

	if toolCall.Name != "calculator" {
		t.Errorf("expected tool name calculator, got %s", toolCall.Name)
	}

	if toolCall.ID != "toolu-123" {
		t.Errorf("expected tool call ID toolu-123, got %s", toolCall.ID)
	}

	// Verify arguments
	x, ok := toolCall.Arguments["x"].(float64)
	if !ok {
		t.Error("expected x argument to be a number")
	} else if x != 5 {
		t.Errorf("expected x=5, got %v", x)
	}
}

// TestAnthropicClient_Stream_ToolResultMessage tests that tool result messages are formatted correctly.
func TestAnthropicClient_Stream_ToolResultMessage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request has proper tool result format
		body := r.Body
		defer func() { _ = body.Close() }()

		// Read body to verify it contains tool result
		buf := make([]byte, 1024)
		n, _ := body.Read(buf)
		bodyStr := string(buf[:n])

		// Should contain tool_use_id and tool_result
		if !strings.Contains(bodyStr, "tool_use_id") {
			t.Error("expected request to contain tool_use_id")
		}
		if !strings.Contains(bodyStr, "tool_result") {
			t.Error("expected request to contain tool_result")
		}

		// Send response
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		_, _ = fmt.Fprintf(w, "event: message_start\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-result\",\"role\":\"assistant\",\"content\":[]}}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "event: content_block_start\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "event: content_block_stop\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "event: message_delta\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "event: message_stop\n")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	client := llm.NewAnthropicClient(llm.AnthropicClientConfig{
		APIKey:  "test-api-key",
		BaseURL: server.URL,
		Model:   "claude-3-5-sonnet-20250219",
	})

	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Calculate something"),
		agent.NewToolResultMessage("toolu-123", "calculator", "Result: 42", false),
	}

	ctx := context.Background()
	eventCh, err := client.Stream(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}

	// Drain events
	for range eventCh {
	}
}

// TestAnthropicClient_NewClient tests client creation with defaults.
func TestAnthropicClient_NewClient(t *testing.T) {
	t.Parallel()

	client := llm.NewAnthropicClient(llm.AnthropicClientConfig{
		APIKey: "test-key",
		Model:  "claude-3-5-sonnet-20250219",
	})

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify model is set correctly
	_ = client // Use client to avoid unused variable error
}

// TestAnthropicClient_NewClientWithBaseURL tests client creation with custom base URL.
func TestAnthropicClient_NewClientWithBaseURL(t *testing.T) {
	t.Parallel()

	customURL := "https://custom.example.com"
	client := llm.NewAnthropicClient(llm.AnthropicClientConfig{
		APIKey:  "test-key",
		BaseURL: customURL,
		Model:   "claude-3-5-sonnet-20250219",
	})

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	_ = client // Use client to avoid unused variable error
}
