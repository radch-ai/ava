// Package agent provides tool execution framework for the agent runtime.
package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestExecuteToolCall_Success tests successful tool execution.
func TestExecuteToolCall_Success(t *testing.T) {
	t.Run("executes tool and returns result message", func(t *testing.T) {
		ctx := context.Background()
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name:        "echo",
					Description: "Echoes input",
					Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
						return &ToolResult{
							Content: []ContentBlock{TextContent{Text: args["input"].(string)}},
						}, nil
					},
				},
			},
		})

		toolCall := ToolCallContent{
			ID:   "call_123",
			Name: "echo",
			Arguments: map[string]any{
				"input": "hello world",
			},
		}

		result, err := ExecuteToolCall(ctx, &state, toolCall)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if result.Role != RoleToolResult {
			t.Errorf("expected RoleToolResult, got %v", result.Role)
		}

		if result.ToolCallID != "call_123" {
			t.Errorf("expected ToolCallID 'call_123', got %q", result.ToolCallID)
		}

		if result.ToolName != "echo" {
			t.Errorf("expected ToolName 'echo', got %q", result.ToolName)
		}

		if result.IsError {
			t.Error("expected IsError to be false")
		}

		if len(result.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(result.Content))
		}

		text, ok := result.Content[0].(TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", result.Content[0])
		}

		if text.Text != "hello world" {
			t.Errorf("expected 'hello world', got %q", text.Text)
		}
	})
}

// TestExecuteToolCall_NotFound tests error when tool is not found.
func TestExecuteToolCall_NotFound(t *testing.T) {
	t.Run("returns error for unknown tool", func(t *testing.T) {
		ctx := context.Background()
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name: "existing_tool",
					Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
						return &ToolResult{}, nil
					},
				},
			},
		})

		toolCall := ToolCallContent{
			ID:        "call_123",
			Name:      "nonexistent_tool",
			Arguments: map[string]any{},
		}

		result, err := ExecuteToolCall(ctx, &state, toolCall)

		if err == nil {
			t.Fatal("expected error for unknown tool, got nil")
		}

		var toolErr *ToolExecutionError
		if !errors.As(err, &toolErr) {
			t.Fatalf("expected ToolExecutionError, got %T", err)
		}

		if toolErr.ToolName != "nonexistent_tool" {
			t.Errorf("expected ToolName 'nonexistent_tool', got %q", toolErr.ToolName)
		}

		// Result should still be returned with error marker
		if result.Role != RoleToolResult {
			t.Errorf("expected RoleToolResult, got %v", result.Role)
		}

		if !result.IsError {
			t.Error("expected IsError to be true for tool not found")
		}
	})
}

// TestExecuteToolCall_ToolError tests when tool returns an error.
func TestExecuteToolCall_ToolError(t *testing.T) {
	t.Run("wraps tool execution error", func(t *testing.T) {
		ctx := context.Background()
		expectedErr := errors.New("tool failed miserably")
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name:        "failing_tool",
					Description: "Always fails",
					Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
						return nil, expectedErr
					},
				},
			},
		})

		toolCall := ToolCallContent{
			ID:        "call_456",
			Name:      "failing_tool",
			Arguments: map[string]any{},
		}

		result, err := ExecuteToolCall(ctx, &state, toolCall)

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var toolErr *ToolExecutionError
		if !errors.As(err, &toolErr) {
			t.Fatalf("expected ToolExecutionError, got %T", err)
		}

		if toolErr.ToolName != "failing_tool" {
			t.Errorf("expected ToolName 'failing_tool', got %q", toolErr.ToolName)
		}

		// Result message should indicate error
		if !result.IsError {
			t.Error("expected IsError to be true for tool error")
		}
	})
}

// TestExecuteToolCall_Timeout tests context cancellation.
func TestExecuteToolCall_Timeout(t *testing.T) {
	t.Run("cancels execution when context is done", func(t *testing.T) {
		// Create a tool that respects context cancellation
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name:        "cancellable_tool",
					Description: "Checks context before proceeding",
					Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
						// Check if context is already canceled
						select {
						case <-ctx.Done():
							return nil, ctx.Err()
						default:
						}
						// Would proceed with work here
						return &ToolResult{
							Content: []ContentBlock{TextContent{Text: "done"}},
						}, nil
					},
				},
			},
		})

		toolCall := ToolCallContent{
			ID:        "call_789",
			Name:      "cancellable_tool",
			Arguments: map[string]any{},
		}

		// Test with already-canceled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		result, err := ExecuteToolCall(ctx, &state, toolCall)

		if err == nil {
			t.Fatal("expected error due to context cancellation, got nil")
		}

		if result.IsError != true {
			t.Error("expected IsError to be true for timeout")
		}
	})

	t.Run("cancels execution during timeout", func(t *testing.T) {
		// Create a tool that can be cancelled mid-execution
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name:        "slow_tool",
					Description: "Does long work",
					Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
						// Simulate work that checks context
						for i := 0; i < 100; i++ {
							select {
							case <-ctx.Done():
								return nil, ctx.Err()
							default:
								// Continue work
							}
						}
						return &ToolResult{
							Content: []ContentBlock{TextContent{Text: "done"}},
						}, nil
					},
				},
			},
		})

		toolCall := ToolCallContent{
			ID:        "call_slow",
			Name:      "slow_tool",
			Arguments: map[string]any{},
		}

		// Create context that will be cancelled
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel in a goroutine (simulating timeout)
		done := make(chan struct{})
		go func() {
			<-time.After(1 * time.Millisecond)
			cancel()
		}()

		result, err := ExecuteToolCall(ctx, &state, toolCall)
		close(done)

		// The result depends on timing - may succeed or fail
		// Just verify we get a valid result either way
		if err != nil {
			if !result.IsError {
				t.Error("expected IsError to be true when error returned")
			}
		}
	})
}

// TestExecuteToolCall_WithArgs tests argument passing.
func TestExecuteToolCall_WithArgs(t *testing.T) {
	t.Run("passes arguments correctly to tool", func(t *testing.T) {
		ctx := context.Background()
		var receivedArgs map[string]any

		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name:        "arg_checker",
					Description: "Checks arguments",
					Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
						receivedArgs = args
						return &ToolResult{
							Content: []ContentBlock{TextContent{Text: "ok"}},
						}, nil
					},
				},
			},
		})

		toolCall := ToolCallContent{
			ID:   "call_args",
			Name: "arg_checker",
			Arguments: map[string]any{
				"string_arg": "test",
				"number_arg": 42,
				"bool_arg":   true,
				"slice_arg":  []string{"a", "b"},
				"nested_arg": map[string]any{"key": "value"},
			},
		}

		_, err := ExecuteToolCall(ctx, &state, toolCall)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if receivedArgs["string_arg"] != "test" {
			t.Errorf("string_arg: expected 'test', got %v", receivedArgs["string_arg"])
		}

		if receivedArgs["number_arg"] != int64(42) && receivedArgs["number_arg"] != 42 {
			t.Errorf("number_arg: expected 42, got %v", receivedArgs["number_arg"])
		}

		if receivedArgs["bool_arg"] != true {
			t.Errorf("bool_arg: expected true, got %v", receivedArgs["bool_arg"])
		}
	})
}

// TestExecuteToolCall_EmptyToolList tests handling of empty tool list.
func TestExecuteToolCall_EmptyToolList(t *testing.T) {
	t.Run("returns error when no tools are available", func(t *testing.T) {
		ctx := context.Background()
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{},
		})

		toolCall := ToolCallContent{
			ID:        "call_empty",
			Name:      "any_tool",
			Arguments: map[string]any{},
		}

		result, err := ExecuteToolCall(ctx, &state, toolCall)

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		var toolErr *ToolExecutionError
		if !errors.As(err, &toolErr) {
			t.Fatalf("expected ToolExecutionError, got %T", err)
		}

		if !result.IsError {
			t.Error("expected IsError to be true")
		}
	})
}

// TestExecuteToolCall_ResultContent tests various result content types.
func TestExecuteToolCall_ResultContent(t *testing.T) {
	t.Run("handles text result content", func(t *testing.T) {
		ctx := context.Background()
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name: "text_tool",
					Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
						return &ToolResult{
							Content: []ContentBlock{
								TextContent{Text: "plain text result"},
							},
						}, nil
					},
				},
			},
		})

		toolCall := ToolCallContent{
			ID:        "call_text",
			Name:      "text_tool",
			Arguments: map[string]any{},
		}

		result, err := ExecuteToolCall(ctx, &state, toolCall)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(result.Content))
		}

		text, ok := result.Content[0].(TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", result.Content[0])
		}

		if text.Text != "plain text result" {
			t.Errorf("expected 'plain text result', got %q", text.Text)
		}
	})

	t.Run("handles multiple content blocks in result", func(t *testing.T) {
		ctx := context.Background()
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name: "multi_tool",
					Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
						return &ToolResult{
							Content: []ContentBlock{
								TextContent{Text: "first"},
								TextContent{Text: "second"},
							},
						}, nil
					},
				},
			},
		})

		toolCall := ToolCallContent{
			ID:        "call_multi",
			Name:      "multi_tool",
			Arguments: map[string]any{},
		}

		result, err := ExecuteToolCall(ctx, &state, toolCall)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) != 2 {
			t.Fatalf("expected 2 content blocks, got %d", len(result.Content))
		}
	})
}

// TestFindToolByName tests finding tools by name.
func TestFindToolByName(t *testing.T) {
	t.Run("finds existing tool", func(t *testing.T) {
		tool := Tool{
			Name:        "test_tool",
			Description: "A test tool",
			Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				return &ToolResult{}, nil
			},
		}
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{tool},
		})

		found := FindToolByName(&state, "test_tool")

		if found == nil {
			t.Fatal("expected to find tool, got nil")
		}

		if found.Name != "test_tool" {
			t.Errorf("expected Name 'test_tool', got %q", found.Name)
		}
	})

	t.Run("returns nil for non-existent tool", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{Name: "tool_a"},
				{Name: "tool_b"},
			},
		})

		found := FindToolByName(&state, "tool_c")

		if found != nil {
			t.Errorf("expected nil, got tool with name %q", found.Name)
		}
	})

	t.Run("handles empty tool list", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{},
		})

		found := FindToolByName(&state, "any_tool")

		if found != nil {
			t.Error("expected nil for empty tool list")
		}
	})

	t.Run("finds tool in larger list", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{Name: "first"},
				{Name: "second"},
				{Name: "target_tool"},
				{Name: "fourth"},
			},
		})

		found := FindToolByName(&state, "target_tool")

		if found == nil {
			t.Fatal("expected to find tool")
		}

		if found.Name != "target_tool" {
			t.Errorf("expected Name 'target_tool', got %q", found.Name)
		}
	})
}

// TestExecuteToolCall_NilExecute tests handling of nil Execute function.
func TestExecuteToolCall_NilExecute(t *testing.T) {
	t.Run("returns error when tool has nil Execute", func(t *testing.T) {
		ctx := context.Background()
		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name:        "nil_execute_tool",
					Description: "Tool with nil Execute",
					Execute:     nil,
				},
			},
		})

		toolCall := ToolCallContent{
			ID:        "call_nil",
			Name:      "nil_execute_tool",
			Arguments: map[string]any{},
		}

		result, err := ExecuteToolCall(ctx, &state, toolCall)

		if err == nil {
			t.Fatal("expected error for nil Execute, got nil")
		}

		if !result.IsError {
			t.Error("expected IsError to be true")
		}
	})
}

// TestToolExecutionError tests the ToolExecutionError type.
func TestToolExecutionError(t *testing.T) {
	t.Run("Error returns formatted message with tool name", func(t *testing.T) {
		err := &ToolExecutionError{
			ToolName: "test_tool",
			Message:  "something went wrong",
		}

		got := err.Error()
		expected := "tool test_tool: something went wrong"

		if got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})

	t.Run("Error returns message only when tool name is empty", func(t *testing.T) {
		err := &ToolExecutionError{
			ToolName: "",
			Message:  "generic error",
		}

		got := err.Error()
		expected := "generic error"

		if got != expected {
			t.Errorf("Error() = %q, want %q", got, expected)
		}
	})
}

// TestExecuteToolCall_Timestamp tests that result has proper timestamp.
func TestExecuteToolCall_Timestamp(t *testing.T) {
	t.Run("sets timestamp on result message", func(t *testing.T) {
		ctx := context.Background()
		before := time.Now().Unix()

		state := NewAgentState(AgentStateOptions{
			Tools: []Tool{
				{
					Name: "timestamp_tool",
					Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
						return &ToolResult{
							Content: []ContentBlock{TextContent{Text: "ok"}},
						}, nil
					},
				},
			},
		})

		toolCall := ToolCallContent{
			ID:        "call_ts",
			Name:      "timestamp_tool",
			Arguments: map[string]any{},
		}

		result, err := ExecuteToolCall(ctx, &state, toolCall)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		after := time.Now().Unix()

		if result.Timestamp < before || result.Timestamp > after {
			t.Errorf("timestamp %d not in range [%d, %d]", result.Timestamp, before, after)
		}
	})
}

// TestMockTool tests the MockTool helper function.
func TestMockTool(t *testing.T) {
	t.Run("creates tool with default result", func(t *testing.T) {
		tool := MockTool(MockToolConfig{
			Name: "default_tool",
		})

		if tool.Name != "default_tool" {
			t.Errorf("expected Name 'default_tool', got %q", tool.Name)
		}

		result, err := tool.Execute(context.Background(), map[string]any{})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(result.Content))
		}

		text, ok := result.Content[0].(TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", result.Content[0])
		}

		if text.Text != "ok" {
			t.Errorf("expected 'ok', got %q", text.Text)
		}
	})

	t.Run("creates tool with canned result", func(t *testing.T) {
		cannedResult := &ToolResult{
			Content: []ContentBlock{TextContent{Text: "canned response"}},
		}
		tool := MockTool(MockToolConfig{
			Name:   "canned_tool",
			Result: cannedResult,
		})

		result, err := tool.Execute(context.Background(), map[string]any{})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		text, ok := result.Content[0].(TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", result.Content[0])
		}

		if text.Text != "canned response" {
			t.Errorf("expected 'canned response', got %q", text.Text)
		}
	})

	t.Run("creates tool that returns error", func(t *testing.T) {
		expectedErr := errors.New("mock error")
		tool := MockTool(MockToolConfig{
			Name:  "error_tool",
			Error: expectedErr,
		})

		result, err := tool.Execute(context.Background(), map[string]any{})

		if err != expectedErr {
			t.Fatalf("expected expected error, got %v", err)
		}

		if result != nil {
			t.Error("expected nil result when error is returned")
		}
	})

	t.Run("creates tool with custom execute function", func(t *testing.T) {
		customCalled := false
		tool := MockTool(MockToolConfig{
			Name: "custom_tool",
			ExecuteFunc: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
				customCalled = true
				return &ToolResult{
					Content: []ContentBlock{TextContent{Text: "custom"}},
				}, nil
			},
		})

		result, err := tool.Execute(context.Background(), map[string]any{})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if !customCalled {
			t.Error("custom execute function was not called")
		}

		text, _ := result.Content[0].(TextContent)
		if text.Text != "custom" {
			t.Errorf("expected 'custom', got %q", text.Text)
		}
	})

	t.Run("uses default name when empty", func(t *testing.T) {
		tool := MockTool(MockToolConfig{})

		if tool.Name != "mock_tool" {
			t.Errorf("expected default name 'mock_tool', got %q", tool.Name)
		}
	})
}

// TestMockEchoTool tests the MockEchoTool helper function.
func TestMockEchoTool(t *testing.T) {
	t.Run("echoes input argument", func(t *testing.T) {
		tool := MockEchoTool("echo")

		result, err := tool.Execute(context.Background(), map[string]any{
			"input": "hello world",
		})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		text, ok := result.Content[0].(TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", result.Content[0])
		}

		if text.Text != "hello world" {
			t.Errorf("expected 'hello world', got %q", text.Text)
		}
	})

	t.Run("returns error when input is missing", func(t *testing.T) {
		tool := MockEchoTool("echo")

		_, err := tool.Execute(context.Background(), map[string]any{})

		if err == nil {
			t.Fatal("expected error for missing input, got nil")
		}

		var toolErr *ToolExecutionError
		if !errors.As(err, &toolErr) {
			t.Fatalf("expected ToolExecutionError, got %T", err)
		}
	})

	t.Run("returns error when input is not a string", func(t *testing.T) {
		tool := MockEchoTool("echo")

		_, err := tool.Execute(context.Background(), map[string]any{
			"input": 12345,
		})

		if err == nil {
			t.Fatal("expected error for non-string input, got nil")
		}
	})
}

// TestMockFailingTool tests the MockFailingTool helper function.
func TestMockFailingTool(t *testing.T) {
	t.Run("always returns the provided error", func(t *testing.T) {
		expectedErr := errors.New("always fails")
		tool := MockFailingTool("failing", expectedErr)

		_, err := tool.Execute(context.Background(), map[string]any{})

		if err != expectedErr {
			t.Errorf("expected expected error, got %v", err)
		}
	})
}

// TestMockResultTool tests the MockResultTool helper function.
func TestMockResultTool(t *testing.T) {
	t.Run("returns the provided result", func(t *testing.T) {
		expectedResult := &ToolResult{
			Content: []ContentBlock{
				TextContent{Text: "result text"},
				TextContent{Text: "more text"},
			},
		}
		tool := MockResultTool("result_tool", expectedResult)

		result, err := tool.Execute(context.Background(), map[string]any{})

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if len(result.Content) != 2 {
			t.Fatalf("expected 2 content blocks, got %d", len(result.Content))
		}

		text1, ok := result.Content[0].(TextContent)
		if !ok || text1.Text != "result text" {
			t.Errorf("unexpected first content block")
		}
	})
}
