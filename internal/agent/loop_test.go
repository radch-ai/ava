// Package agent provides the agent runtime loop for tool calling and message processing.
package agent

import (
	"context"
	"testing"
	"time"
)

func TestLoopWithNoToolCalls(t *testing.T) {
	t.Run("exits after text response without looping", func(t *testing.T) {
		ctx := context.Background()

		// Mock LLM returns text-only response - no tool calls
		mock := &MockLLMClient{
			Responses: []MockResponse{
				{
					// Text response with no tool calls means loop exits
					Message: AgentMessage{
						Role:      RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content:   []ContentBlock{TextContent{Text: "Hello, world!"}},
					},
				},
			},
		}

		loop := NewLoop(mock)
		initialMessages := []AgentMessage{
			NewTextMessage(RoleUser, "Hi there"),
		}

		result, err := loop.Run(ctx, initialMessages)

		// Should complete without error
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Should have initial user message + assistant response
		if len(result.Messages) != 2 {
			t.Errorf("expected 2 messages in history, got %d", len(result.Messages))
		}

		// Final message should be from assistant
		finalMsg := result.Messages[len(result.Messages)-1]
		if finalMsg.Role != RoleAssistant {
			t.Errorf("expected final message role %v, got %v", RoleAssistant, finalMsg.Role)
		}

		// Final message should contain the text response
		if len(finalMsg.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(finalMsg.Content))
		}
		text, ok := finalMsg.Content[0].(TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", finalMsg.Content[0])
		}
		if text.Text != "Hello, world!" {
			t.Errorf("expected text 'Hello, world!', got %q", text.Text)
		}
	})
}

func TestLoopWithSingleToolCall(t *testing.T) {
	t.Run("executes tool and loops for final response", func(t *testing.T) {
		ctx := context.Background()

		toolCalled := false
		testTool := Tool{
			Name:        "test_tool",
			Description: "A test tool",
			Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				toolCalled = true
				return &ToolResult{
					Content: []ContentBlock{TextContent{Text: "Tool executed successfully"}},
				}, nil
			},
		}

		// First response: tool call
		// Second response: final text after tool execution
		mock := &MockLLMClient{
			Responses: []MockResponse{
				{
					Message: AgentMessage{
						Role:      RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content: []ContentBlock{
							ToolCallContent{
								ID:   "call_123",
								Name: "test_tool",
								Arguments: map[string]any{
									"param": "value",
								},
							},
						},
					},
				},
				{
					Message: AgentMessage{
						Role:      RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content:   []ContentBlock{TextContent{Text: "Tool execution complete"}},
					},
				},
			},
		}

		loop := NewLoop(mock)
		initialMessages := []AgentMessage{
			NewTextMessage(RoleUser, "Run the tool"),
		}

		result, err := loop.Run(ctx, initialMessages, testTool)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Verify tool was called
		if !toolCalled {
			t.Error("expected tool to be called")
		}

		// Should have: user, tool call, tool result, final response
		if len(result.Messages) != 4 {
			t.Errorf("expected 4 messages, got %d", len(result.Messages))
		}

		// Verify tool result message exists
		var toolResultMsg *AgentMessage
		for i := range result.Messages {
			if result.Messages[i].Role == RoleToolResult {
				toolResultMsg = &result.Messages[i]
				break
			}
		}
		if toolResultMsg == nil {
			t.Fatal("expected a tool result message in history")
		}
		if toolResultMsg.ToolName != "test_tool" {
			t.Errorf("expected tool name 'test_tool', got %q", toolResultMsg.ToolName)
		}
	})
}

func TestLoopWithMultipleToolCalls(t *testing.T) {
	t.Run("executes multiple tools in single response", func(t *testing.T) {
		ctx := context.Background()

		calls := make(map[string]bool)
		tool1 := Tool{
			Name: "tool_one",
			Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				calls["tool_one"] = true
				return &ToolResult{
					Content: []ContentBlock{TextContent{Text: "Tool 1 result"}},
				}, nil
			},
		}
		tool2 := Tool{
			Name: "tool_two",
			Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				calls["tool_two"] = true
				return &ToolResult{
					Content: []ContentBlock{TextContent{Text: "Tool 2 result"}},
				}, nil
			},
		}

		// Response with two tool calls, then final text response
		mock := &MockLLMClient{
			Responses: []MockResponse{
				{
					Message: AgentMessage{
						Role:      RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content: []ContentBlock{
							ToolCallContent{
								ID:   "call_1",
								Name: "tool_one",
								Arguments: map[string]any{
									"arg": "1",
								},
							},
							ToolCallContent{
								ID:   "call_2",
								Name: "tool_two",
								Arguments: map[string]any{
									"arg": "2",
								},
							},
						},
					},
				},
				{
					Message: AgentMessage{
						Role:      RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content:   []ContentBlock{TextContent{Text: "Both tools executed"}},
					},
				},
			},
		}

		loop := NewLoop(mock)
		result, err := loop.Run(ctx, []AgentMessage{
			NewTextMessage(RoleUser, "Run both tools"),
		}, tool1, tool2)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Both tools should be called
		if !calls["tool_one"] {
			t.Error("expected tool_one to be called")
		}
		if !calls["tool_two"] {
			t.Error("expected tool_two to be called")
		}

		// Messages: user, tool calls (1 msg with 2 tool calls), tool results (2), final response
		// Actually tool calls are in one message, tool results are separate messages
		// So: user, tool-call-msg, tool-result-1, tool-result-2, final-response = 5 messages
		if len(result.Messages) != 5 {
			t.Errorf("expected 5 messages, got %d", len(result.Messages))
		}
	})
}

func TestLoopWithToolError(t *testing.T) {
	t.Run("handles tool execution errors gracefully", func(t *testing.T) {
		ctx := context.Background()

		expectedErr := &ToolExecutionError{
			ToolName: "failing_tool",
			Message:  "tool failed",
		}

		failingTool := Tool{
			Name: "failing_tool",
			Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				return nil, expectedErr
			},
		}

		mock := &MockLLMClient{
			Responses: []MockResponse{
				{
					Message: AgentMessage{
						Role:      RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content: []ContentBlock{
							ToolCallContent{
								ID:        "call_error",
								Name:      "failing_tool",
								Arguments: map[string]any{},
							},
						},
					},
				},
				{
					Message: AgentMessage{
						Role:      RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content:   []ContentBlock{TextContent{Text: "I see there was an error"}},
					},
				},
			},
		}

		loop := NewLoop(mock)
		result, err := loop.Run(ctx, []AgentMessage{
			NewTextMessage(RoleUser, "Run failing tool"),
		}, failingTool)

		if err != nil {
			t.Fatalf("expected no error from loop, got %v", err)
		}

		// Find the tool result message
		var toolResultMsg *AgentMessage
		for i := range result.Messages {
			if result.Messages[i].Role == RoleToolResult {
				toolResultMsg = &result.Messages[i]
				break
			}
		}

		if toolResultMsg == nil {
			t.Fatal("expected a tool result message")
		}

		// Tool result should be marked as error
		if !toolResultMsg.IsError {
			t.Error("expected tool result to be marked as error")
		}
	})
}

func TestLoopCancellation(t *testing.T) {
	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Mock that will block until cancelled
		blockChan := make(chan struct{})
		mock := &MockLLMClient{
			Responses: []MockResponse{
				{
					Message: AgentMessage{
						Role:      RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content:   []ContentBlock{TextContent{Text: "Response"}},
					},
					BlockUntil: blockChan,
				},
			},
		}

		loop := NewLoop(mock)

		// Start loop in goroutine
		errChan := make(chan error, 1)
		go func() {
			_, err := loop.Run(ctx, []AgentMessage{
				NewTextMessage(RoleUser, "Hello"),
			})
			errChan <- err
		}()

		// Cancel context immediately
		cancel()

		// Should receive an error (context.Canceled or similar)
		err := <-errChan
		if err == nil {
			t.Error("expected error on context cancellation, got nil")
		}
		if err != context.Canceled {
			t.Logf("got error: %v (context.Canceled is acceptable too)", err)
		}

		close(blockChan)
	})
}

func TestLoopExtractToolCalls(t *testing.T) {
	t.Run("extracts tool calls from assistant message", func(t *testing.T) {
		msg := AgentMessage{
			Role:      RoleAssistant,
			Timestamp: time.Now().Unix(),
			Content: []ContentBlock{
				TextContent{Text: "I'll help you"},
				ToolCallContent{
					ID:   "call_1",
					Name: "search",
					Arguments: map[string]any{
						"query": "golang",
					},
				},
				ToolCallContent{
					ID:   "call_2",
					Name: "calculate",
					Arguments: map[string]any{
						"expression": "2+2",
					},
				},
			},
		}

		loop := NewLoop(nil)
		calls := loop.ExtractToolCalls(msg)

		if len(calls) != 2 {
			t.Fatalf("expected 2 tool calls, got %d", len(calls))
		}

		if calls[0].ID != "call_1" {
			t.Errorf("expected ID 'call_1', got %q", calls[0].ID)
		}
		if calls[0].Name != "search" {
			t.Errorf("expected name 'search', got %q", calls[0].Name)
		}
		if calls[1].ID != "call_2" {
			t.Errorf("expected ID 'call_2', got %q", calls[1].ID)
		}
	})

	t.Run("returns empty slice for message without tool calls", func(t *testing.T) {
		msg := NewTextMessage(RoleAssistant, "No tools here")

		loop := NewLoop(nil)
		calls := loop.ExtractToolCalls(msg)

		if len(calls) != 0 {
			t.Errorf("expected 0 tool calls, got %d", len(calls))
		}
	})

	t.Run("returns empty slice for non-assistant message", func(t *testing.T) {
		msg := AgentMessage{
			Role:      RoleUser,
			Timestamp: time.Now().Unix(),
			Content: []ContentBlock{
				ToolCallContent{
					ID:        "call_1",
					Name:      "some_tool",
					Arguments: map[string]any{},
				},
			},
		}

		loop := NewLoop(nil)
		calls := loop.ExtractToolCalls(msg)

		// Tool calls from user messages should be ignored
		if len(calls) != 0 {
			t.Errorf("expected 0 tool calls from user message, got %d", len(calls))
		}
	})
}

func TestLoopAddToolResults(t *testing.T) {
	t.Run("adds tool result messages to history", func(t *testing.T) {
		loop := NewLoop(nil)
		messages := []AgentMessage{
			NewTextMessage(RoleUser, "Hello"),
		}

		results := []ToolExecutionResult{
			{
				ToolCallID: "call_1",
				ToolName:   "test_tool",
				Result: &ToolResult{
					Content: []ContentBlock{TextContent{Text: "Success"}},
				},
				IsError: false,
			},
			{
				ToolCallID: "call_2",
				ToolName:   "failing_tool",
				Error:      &ToolExecutionError{ToolName: "failing_tool", Message: "failed"},
				IsError:    true,
			},
		}

		updated := loop.AddToolResults(messages, results)

		// Should have original + 2 tool result messages
		if len(updated) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(updated))
		}

		// Check first tool result
		if updated[1].Role != RoleToolResult {
			t.Errorf("expected role %v, got %v", RoleToolResult, updated[1].Role)
		}
		if updated[1].ToolCallID != "call_1" {
			t.Errorf("expected tool call ID 'call_1', got %q", updated[1].ToolCallID)
		}
		if updated[1].IsError {
			t.Error("expected first result to not be error")
		}

		// Check second tool result
		if updated[2].Role != RoleToolResult {
			t.Errorf("expected role %v, got %v", RoleToolResult, updated[2].Role)
		}
		if updated[2].ToolCallID != "call_2" {
			t.Errorf("expected tool call ID 'call_2', got %q", updated[2].ToolCallID)
		}
		if !updated[2].IsError {
			t.Error("expected second result to be error")
		}
	})
}
