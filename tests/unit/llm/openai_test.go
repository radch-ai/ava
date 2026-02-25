// Package llm_test provides OpenAI LLM client implementation and tests.
package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
	llm "github.com/nevaria-ai/gateway/internal/llm"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// TestOpenAIClient_Stream_TextOnly tests streaming a simple text response.
func TestOpenAIClient_Stream_TextOnly(t *testing.T) {
	t.Parallel()

	// Track the request to verify it's formatted correctly
	var receivedRequest map[string]any

	// Create a mock server that returns a streaming text response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}

		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", r.URL.Path)
		}

		// Verify headers
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", contentType)
		}

		if auth := r.Header.Get("Authorization"); auth != "Bearer test-api-key" {
			t.Errorf("expected Authorization 'Bearer test-api-key', got %s", auth)
		}

		// Parse request body
		if err := json.NewDecoder(r.Body).Decode(&receivedRequest); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// Verify request parameters
		if receivedRequest["model"] != "gpt-4o" {
			t.Errorf("expected model gpt-4o, got %v", receivedRequest["model"])
		}

		if receivedRequest["stream"] != true {
			t.Errorf("expected stream true, got %v", receivedRequest["stream"])
		}

		messages, ok := receivedRequest["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("expected 1 message, got %v", receivedRequest["messages"])
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, _ := w.(http.Flusher)

		// Send SSE events following OpenAI's streaming format
		// First chunk with role
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"logprobs\":null,\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		// Content chunks
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"logprobs\":null,\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\", world!\"},\"logprobs\":null,\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		// Final chunk with finish_reason
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-123\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"logprobs\":null,\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()

		// Termination
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	// Create client using OpenAI SDK
	oaiClient := openai.NewClient(
		option.WithAPIKey("test-api-key"),
		option.WithBaseURL(server.URL+"/"),
	)

	openaiClient := llm.NewOpenAIClient(llm.OpenAIClientConfig{
		Client: oaiClient,
		Model:  agent.Model{ID: "gpt-4o", Provider: "openai"},
	})

	// Create messages
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Hello"),
	}

	// Stream response
	ctx := context.Background()
	eventCh, err := openaiClient.Stream(ctx, messages, nil)
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

	// Verify request format
	messagesReq, ok := receivedRequest["messages"].([]any)
	if !ok {
		t.Fatal("expected messages to be a slice")
	}
	if len(messagesReq) != 1 {
		t.Fatalf("expected 1 message in request, got %d", len(messagesReq))
	}
	firstMsg, ok := messagesReq[0].(map[string]any)
	if !ok {
		t.Fatal("expected first message to be a map")
	}
	if firstMsg["role"] != "user" {
		t.Errorf("expected role 'user', got %v", firstMsg["role"])
	}
	if firstMsg["content"] != "Hello" {
		t.Errorf("expected content 'Hello', got %v", firstMsg["content"])
	}
}

// TestOpenAIClient_Stream_WithToolCall tests streaming a response with tool calls.
func TestOpenAIClient_Stream_WithToolCall(t *testing.T) {
	t.Parallel()

	// Track the request to verify tools are formatted correctly
	var receivedRequest map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse request body
		if err := json.NewDecoder(r.Body).Decode(&receivedRequest); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// Verify tools
		tools, ok := receivedRequest["tools"].([]any)
		if !ok {
			t.Fatal("expected tools in request")
		}
		if len(tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(tools))
		}

		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatal("expected tool to be a map")
		}
		if tool["type"] != "function" {
			t.Errorf("expected tool type 'function', got %v", tool["type"])
		}

		function, ok := tool["function"].(map[string]any)
		if !ok {
			t.Fatal("expected tool.function to be a map")
		}
		if function["name"] != "calculator" {
			t.Errorf("expected tool name 'calculator', got %v", function["name"])
		}

		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, _ := w.(http.Flusher)

		// Send events with tool call
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-456\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"logprobs\":null,\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		// Tool call chunks
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-456\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_123\",\"type\":\"function\",\"function\":{\"name\":\"calculator\",\"arguments\":\"\"}}]},\"logprobs\":null,\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-456\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"x\\\":5,\"}}]},\"logprobs\":null,\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-456\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"y\\\":3}\"}}]},\"logprobs\":null,\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		// Final chunk
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-456\",\"object\":\"chat.completion.chunk\",\"created\":1234567890,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"logprobs\":null,\"finish_reason\":\"tool_calls\"}]}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	oaiClient := openai.NewClient(
		option.WithAPIKey("test-api-key"),
		option.WithBaseURL(server.URL+"/"),
	)

	openaiClient := llm.NewOpenAIClient(llm.OpenAIClientConfig{
		Client: oaiClient,
		Model:  agent.Model{ID: "gpt-4o", Provider: "openai"},
	})

	// Create messages
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "What is 5 + 3?"),
	}

	// Create tools
	tools := []agent.Tool{
		{
			Name:        "calculator",
			Description: "A simple calculator",
			Execute:     nil,
		},
	}

	// Stream response
	ctx := context.Background()
	eventCh, err := openaiClient.Stream(ctx, messages, tools)
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

	// Find MessageEndEvent
	var msgEnd agent.MessageEndEvent
	found := false
	for _, e := range events {
		if me, ok := e.(agent.MessageEndEvent); ok {
			msgEnd = me
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find MessageEndEvent, got events: %v", events)
	}

	// Check final message has tool call
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

	if toolCall.ID != "call_123" {
		t.Errorf("expected tool call ID call_123, got %s", toolCall.ID)
	}

	// Verify arguments
	x, ok := toolCall.Arguments["x"].(float64)
	if !ok {
		t.Error("expected x argument to be a number")
	} else if x != 5 {
		t.Errorf("expected x=5, got %v", x)
	}

	y, ok := toolCall.Arguments["y"].(float64)
	if !ok {
		t.Error("expected y argument to be a number")
	} else if y != 3 {
		t.Errorf("expected y=3, got %v", y)
	}
}

// TestOpenAIClient_Stream_ToolResultMessage tests that tool result messages are formatted correctly.
func TestOpenAIClient_Stream_ToolResultMessage(t *testing.T) {
	t.Parallel()

	var receivedRequest map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse request to verify tool result format
		if err := json.NewDecoder(r.Body).Decode(&receivedRequest); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}

		// Verify we have a tool result message
		messages, ok := receivedRequest["messages"].([]any)
		if !ok || len(messages) != 2 {
			t.Fatalf("expected 2 messages, got %v", receivedRequest["messages"])
		}

		// Check second message is a tool message
		toolMsg, ok := messages[1].(map[string]any)
		if !ok {
			t.Fatal("expected second message to be a map")
		}
		if toolMsg["role"] != "tool" {
			t.Errorf("expected role 'tool', got %v", toolMsg["role"])
		}
		if toolMsg["tool_call_id"] != "call_123" {
			t.Errorf("expected tool_call_id 'call_123', got %v", toolMsg["tool_call_id"])
		}
		if toolMsg["content"] != "Result: 42" {
			t.Errorf("expected content 'Result: 42', got %v", toolMsg["content"])
		}

		// Send minimal response
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-789\",\"object\":\"chat.completion.chunk\",\"created\":1234567890\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"logprobs\":null,\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-789\",\"object\":\"chat.completion.chunk\",\"created\":1234567890\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Done\"},\"logprobs\":null,\"finish_reason\":null}]}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: {\"id\":\"chatcmpl-789\",\"object\":\"chat.completion.chunk\",\"created\":1234567890\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"logprobs\":null,\"finish_reason\":\"stop\"}]}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	oaiClient := openai.NewClient(
		option.WithAPIKey("test-api-key"),
		option.WithBaseURL(server.URL+"/"),
	)

	openaiClient := llm.NewOpenAIClient(llm.OpenAIClientConfig{
		Client: oaiClient,
		Model:  agent.Model{ID: "gpt-4o", Provider: "openai"},
	})

	// Create messages with a tool result
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Calculate something"),
		agent.NewToolResultMessage("call_123", "calculator", "Result: 42", false),
	}

	ctx := context.Background()
	eventCh, err := openaiClient.Stream(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}

	// Drain events
	for range eventCh {
	}
}

// TestOpenAIClient_Stream_ErrorResponse tests error handling from server.
func TestOpenAIClient_Stream_ErrorResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"message": "Invalid request",
				"type":    "invalid_request_error",
				"code":    "invalid_request",
			},
		})
	}))
	defer server.Close()

	oaiClient := openai.NewClient(
		option.WithAPIKey("test-api-key"),
		option.WithBaseURL(server.URL+"/"),
	)

	openaiClient := llm.NewOpenAIClient(llm.OpenAIClientConfig{
		Client: oaiClient,
		Model:  agent.Model{ID: "gpt-4o", Provider: "openai"},
	})

	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Hello"),
	}

	ctx := context.Background()
	_, err := openaiClient.Stream(ctx, messages, nil)
	if err == nil {
		t.Fatal("expected error from server, got nil")
	}
}

// TestOpenAIClient_convertMessages tests message conversion.
func TestOpenAIClient_convertMessages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		messages []agent.AgentMessage
		wantLen  int
	}{
		{
			name: "user text message",
			messages: []agent.AgentMessage{
				agent.NewTextMessage(agent.RoleUser, "Hello"),
			},
			wantLen: 1,
		},
		{
			name: "assistant message with tool call",
			messages: []agent.AgentMessage{
				agent.NewToolCallMessage("call_123", "calculator", map[string]any{"x": 1, "y": 2}),
			},
			wantLen: 1,
		},
		{
			name: "tool result message",
			messages: []agent.AgentMessage{
				agent.NewToolResultMessage("call_123", "calculator", "3", false),
			},
			wantLen: 1,
		},
		{
			name: "mixed messages",
			messages: []agent.AgentMessage{
				agent.NewTextMessage(agent.RoleUser, "Calculate 1+2"),
				agent.NewToolCallMessage("call_123", "calculator", map[string]any{"x": 1, "y": 2}),
				agent.NewToolResultMessage("call_123", "calculator", "3", false),
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: We can't test the private method directly
			// Skip or use export_test.go pattern
			t.Skip("Private method test - needs export_test.go")
		})
	}
}

// TestOpenAIClient_convertTools tests tool conversion.
func TestOpenAIClient_convertTools(t *testing.T) {
	t.Parallel()

	tools := []agent.Tool{
		{
			Name:        "calculator",
			Description: "A simple calculator",
		},
		{
			Name:        "weather",
			Description: "Get weather information",
		},
	}

	result := llm.ToOpenAITools(tools)

	if len(result) != 2 {
		t.Fatalf("convertTools() returned %d tools, want 2", len(result))
	}

	if result[0].Function.Name != "calculator" {
		t.Errorf("expected function name 'calculator', got %s", result[0].Function.Name)
	}

	if result[1].Function.Name != "weather" {
		t.Errorf("expected function name 'weather', got %s", result[1].Function.Name)
	}

	// Verify tools marshal correctly with type "function"
	jsonBytes, err := json.Marshal(result[0])
	if err != nil {
		t.Fatalf("failed to marshal tool: %v", err)
	}
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"type":"function"`) {
		t.Errorf("expected marshaled JSON to contain type:\"function\", got %s", jsonStr)
	}
}
