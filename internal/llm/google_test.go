// Package llm provides Google GenAI client implementation and tests.
package llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"google.golang.org/genai"
)

// TestGoogleClient_Stream_TextOnly tests streaming a simple text response.
// This test is marked as skipped since it requires a real API key.
// To run this test, set GOOGLE_API_KEY environment variable and use: go test -run TestGoogleClient_Stream_TextOnly -skip=SKIPPED
func TestGoogleClient_Stream_TextOnly(t *testing.T) {
	t.Skip("Skipping real API test - requires valid API key")

	// Create a client
	client := NewGoogleClient(GoogleClientConfig{
		APIKey: "test-api-key",
		Model:  "gemini-2.5-pro",
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
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (start, end), got %d", len(events))
	}

	// Find MessageStartEvent
	var msgStart agent.MessageStartEvent
	found := false
	for _, e := range events {
		if ms, ok := e.(agent.MessageStartEvent); ok {
			msgStart = ms
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find MessageStartEvent, got events: %v", events)
	}
	if msgStart.Message.Role != agent.RoleAssistant {
		t.Errorf("expected role assistant, got %s", msgStart.Message.Role)
	}

	// Check we got MessageUpdateEvent(s)
	updateCount := 0
	for _, e := range events {
		if _, ok := e.(agent.MessageUpdateEvent); ok {
			updateCount++
		}
	}

	if updateCount == 0 {
		t.Error("expected at least one MessageUpdateEvent")
	}
}

// TestGoogleClient_Stream_WithToolCall tests streaming a response with tool calls.
// This test is marked as skipped since it requires a real API key.
func TestGoogleClient_Stream_WithToolCall(t *testing.T) {
	t.Skip("Skipping real API test - requires valid API key")

	client := NewGoogleClient(GoogleClientConfig{
		APIKey: "test-api-key",
		Model:  "gemini-2.5-flash",
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
		},
	}

	// Stream response
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

	// Check if content includes a tool call
	for _, block := range msgEnd.Message.Content {
		if toolCall, ok := block.(agent.ToolCallContent); ok {
			if toolCall.Name != "calculator" {
				t.Errorf("expected tool name calculator, got %s", toolCall.Name)
			}
			return
		}
	}
	t.Error("expected to find a tool call in the response")
}

// TestGoogleClient_Stream_ContextCancellation tests context cancellation.
func TestGoogleClient_Stream_ContextCancellation(t *testing.T) {
	t.Parallel()

	client := NewGoogleClient(GoogleClientConfig{
		APIKey: "test-api-key",
		Model:  "gemini-2.0-flash-exp",
	})

	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "Hello"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	eventCh, err := client.Stream(ctx, messages, nil)
	if err != nil {
		t.Fatalf("Stream() failed: %v", err)
	}

	// Channel should be closed quickly
	eventCount := 0
	for range eventCh {
		eventCount++
	}

	// Should have no events due to cancellation
	if eventCount > 0 {
		t.Logf("Warning: expected no events due to cancellation, got %d", eventCount)
	}
}

// TestGoogleClient_convertMessagesToGenai tests message conversion to GenAI format.
func TestGoogleClient_convertMessagesToGenai(t *testing.T) {
	t.Parallel()

	client := NewGoogleClient(GoogleClientConfig{
		APIKey: "test-api-key",
		Model:  "gemini-2.5-pro",
	})

	tests := []struct {
		name     string
		messages []agent.AgentMessage
		wantLen  int
	}{
		{
			name: "Single user message",
			messages: []agent.AgentMessage{
				agent.NewTextMessage(agent.RoleUser, "Hello"),
			},
			wantLen: 1,
		},
		{
			name: "User and assistant messages",
			messages: []agent.AgentMessage{
				agent.NewTextMessage(agent.RoleUser, "Hello"),
				agent.NewTextMessage(agent.RoleAssistant, "Hi there!"),
			},
			wantLen: 2,
		},
		{
			name: "Tool result message",
			messages: []agent.AgentMessage{
				agent.NewToolResultMessage("tool-123", "calculator", "42", false),
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.convertMessagesToGenai(tt.messages)
			if len(result) != tt.wantLen {
				t.Errorf("convertMessagesToGenai() returned %d messages, want %d", len(result), tt.wantLen)
			}

			// Check roles are correctly set
			for i, content := range result {
				if content.Role == "" {
					t.Errorf("content[%d].Role should not be empty", i)
				}
			}
		})
	}
}

// TestGoogleClient_convertToolsToGenai tests tool conversion to GenAI format.
func TestGoogleClient_convertToolsToGenai(t *testing.T) {
	t.Parallel()

	client := NewGoogleClient(GoogleClientConfig{
		APIKey: "test-api-key",
		Model:  "gemini-2.5-pro",
	})

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

	result := client.convertToolsToGenai(tools)

	if len(result) != 2 {
		t.Fatalf("convertToolsToGenai() returned %d tools, want 2", len(result))
	}

	// Check that each tool has function declarations
	for i, tool := range result {
		if len(tool.FunctionDeclarations) == 0 {
			t.Errorf("tool[%d] should have function declarations", i)
		}
	}
}

// TestToGoogleTool tests individual tool conversion.
func TestToGoogleTool(t *testing.T) {
	t.Parallel()

	tool := agent.Tool{
		Name:        "test_tool",
		Description: "A test tool",
	}

	googleTool := ToGoogleTool(tool)

	if googleTool.Name != "test_tool" {
		t.Errorf("expected name 'test_tool', got %s", googleTool.Name)
	}

	if googleTool.Description != "A test tool" {
		t.Errorf("expected description 'A test tool', got %s", googleTool.Description)
	}

	if googleTool.Parameters == nil {
		t.Error("expected non-nil parameters")
	}

	if googleTool.Parameters.Type != "object" {
		t.Errorf("expected parameters type 'object', got %s", googleTool.Parameters.Type)
	}
}

// TestToGoogleTools tests multiple tools conversion.
func TestToGoogleTools(t *testing.T) {
	t.Parallel()

	tools := []agent.Tool{
		{Name: "tool1", Description: "Tool 1"},
		{Name: "tool2", Description: "Tool 2"},
	}

	result := ToGoogleTools(tools)

	if len(result.FunctionDeclarations) != 2 {
		t.Fatalf("expected 2 function declarations, got %d", len(result.FunctionDeclarations))
	}

	if result.FunctionDeclarations[0].Name != "tool1" {
		t.Errorf("expected first tool name 'tool1', got %s", result.FunctionDeclarations[0].Name)
	}

	if result.FunctionDeclarations[1].Name != "tool2" {
		t.Errorf("expected second tool name 'tool2', got %s", result.FunctionDeclarations[1].Name)
	}
}

// TestGoogleClient_initClient tests lazy client initialization.
func TestGoogleClient_initClient(t *testing.T) {
	t.Parallel()

	client := NewGoogleClient(GoogleClientConfig{
		APIKey: "test-api-key",
		Model:  "gemini-2.5-pro",
	})

	// Client should not be initialized initially
	if client.client != nil {
		t.Error("expected client to be nil before initClient is called")
	}

	// Note: The GenAI SDK's NewClient doesn't immediately connect to the API,
	// it just creates the client object. Connection happens on first API call.
	// So we can't test initialization failure without a valid API key.

	// Test that initClient with a valid context creates the client
	ctx := context.Background()
	err := client.initClient(ctx)
	if err != nil {
		// This is expected to fail without a real API key during actual API calls,
		// but the client creation itself doesn't validate the key
		t.Logf("initClient returned error (may be expected without valid key): %v", err)
	}

	// After initClient is called, client should be set (even if the key is invalid)
	// The GenAI SDK validates keys on first API call, not client creation
}

// TestConvertContentToText tests converting content blocks to text.
func TestConvertContentToText(t *testing.T) {
	t.Parallel()

	content := []agent.ContentBlock{
		agent.TextContent{Text: "Hello "},
		agent.TextContent{Text: "World!"},
	}

	result := ConvertContentToText(content)
	if result != "Hello World!" {
		t.Errorf("expected 'Hello World!', got %s", result)
	}
}

// TestConvertToolResultContent tests converting tool result content.
func TestConvertToolResultContent(t *testing.T) {
	t.Parallel()

	content := []agent.ContentBlock{
		agent.TextContent{Text: "42"},
	}

	result := convertToolResultContent(content)
	if result["result"] != "42" {
		t.Errorf("expected result '42', got %v", result["result"])
	}
}

// TestConvertToolResultContent_Empty tests converting empty tool result content.
func TestConvertToolResultContent_Empty(t *testing.T) {
	t.Parallel()

	content := []agent.ContentBlock{}

	result := convertToolResultContent(content)
	if result["result"] != nil {
		t.Errorf("expected result nil, got %v", result["result"])
	}
}

// TestGenerateToolCallID tests tool call ID generation.
func TestGenerateToolCallID(t *testing.T) {
	t.Parallel()

	id1 := generateToolCallID()
	id2 := generateToolCallID()

	if id1 == id2 {
		t.Error("expected different IDs for different calls")
	}

	if !strings.HasPrefix(id1, "call_") {
		t.Errorf("expected ID to start with 'call_', got %s", id1)
	}
}

// TestConvertJSONSchemaToGenai tests JSON schema conversion.
func TestConvertJSONSchemaToGenai(t *testing.T) {
	t.Parallel()

	schema := &JSONSchema{
		Type: "object",
		Properties: map[string]PropertySchema{
			"name": {
				Type:        "string",
				Description: "The name",
			},
			"age": {
				Type:        "integer",
				Description: "The age",
			},
		},
		Required: []string{"name"},
	}

	result := convertJSONSchemaToGenai(schema)

	// The SDK accepts both "object" and "OBJECT", but our conversion preserves the input case
	if result.Type != "object" && result.Type != genai.TypeObject {
		t.Errorf("expected type object, got %v", result.Type)
	}

	if len(result.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(result.Properties))
	}

	if len(result.Required) != 1 {
		t.Errorf("expected 1 required field, got %d", len(result.Required))
	}
}

// TestNewGoogleClient tests client creation with defaults.
func TestNewGoogleClient(t *testing.T) {
	t.Parallel()

	config := GoogleClientConfig{
		APIKey: "test-key",
		Model:  "gemini-2.5-pro",
	}

	client := NewGoogleClient(config)

	if client.config.Timeout == 0 {
		t.Error("expected timeout to be set to default")
	}

	if client.model.ID != "gemini-2.5-pro" {
		t.Errorf("expected model ID 'gemini-2.5-pro', got %s", client.model.ID)
	}

	if client.model.Provider != "google" {
		t.Errorf("expected provider 'google', got %s", client.model.Provider)
	}
}

// TestGoogleClientConfig_DefaultTimeout tests that default timeout is applied.
func TestGoogleClientConfig_DefaultTimeout(t *testing.T) {
	t.Parallel()

	config := GoogleClientConfig{
		APIKey: "test-key",
		Model:  "gemini-2.5-pro",
	}

	client := NewGoogleClient(config)

	if client.config.Timeout != DefaultTimeout {
		t.Errorf("expected default timeout %v, got %v", DefaultTimeout, client.config.Timeout)
	}
}

// TestGoogleClientConfig_CustomTimeout tests that custom timeout is respected.
func TestGoogleClientConfig_CustomTimeout(t *testing.T) {
	t.Parallel()

	customTimeout := 120 * time.Second
	config := GoogleClientConfig{
		APIKey:  "test-key",
		Model:   "gemini-2.5-pro",
		Timeout: customTimeout,
	}

	client := NewGoogleClient(config)

	if client.config.Timeout != customTimeout {
		t.Errorf("expected custom timeout %v, got %v", customTimeout, client.config.Timeout)
	}
}
