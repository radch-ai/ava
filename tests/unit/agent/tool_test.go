// Package agent_test provides tests for the agent tool execution framework.
package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// TestExecuteToolCall_Success tests successful tool execution.
func TestExecuteToolCall_Success(t *testing.T) {
	t.Run("executes tool and returns result message", func(t *testing.T) {
		ctx := context.Background()
		state := agent.NewAgentState(agent.AgentStateOptions{
			Tools: []agent.Tool{
				{
					Name:        "echo",
					Description: "Echoes input",
					Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
						return &agent.ToolResult{
							Content: []agent.ContentBlock{agent.TextContent{Text: args["input"].(string)}},
						}, nil
					},
				},
			},
		})

		toolCall := agent.ToolCallContent{
			ID:   "call_123",
			Name: "echo",
			Arguments: map[string]any{
				"input": "hello world",
			},
		}

		result, err := agent.ExecuteToolCall(ctx, &state, toolCall)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if result.Role != agent.RoleToolResult {
			t.Errorf("expected RoleToolResult, got %v", result.Role)
		}

		if result.ToolCallID != "call_123" {
			t.Errorf("expected ToolCallID 'call_123', got %q", result.ToolCallID)
		}

		if len(result.Content) == 0 {
			t.Fatal("expected result to have content")
		}

		text, ok := result.Content[0].(agent.TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", result.Content[0])
		}

		if text.Text != "hello world" {
			t.Errorf("expected text 'hello world', got %q", text.Text)
		}
	})
}

// TestExecuteToolCall_ToolNotFound tests behavior when tool doesn't exist.
func TestExecuteToolCall_ToolNotFound(t *testing.T) {
	t.Run("returns error message for unknown tool", func(t *testing.T) {
		ctx := context.Background()
		state := agent.NewAgentState(agent.AgentStateOptions{
			Tools: []agent.Tool{
				{Name: "other_tool"},
			},
		})

		toolCall := agent.ToolCallContent{
			ID:        "call_123",
			Name:      "unknown_tool",
			Arguments: map[string]any{},
		}

		result, err := agent.ExecuteToolCall(ctx, &state, toolCall)

		// Should return both an error result message AND an error
		if err == nil {
			t.Fatal("expected error for unknown tool, got nil")
		}

		if result.Role != agent.RoleToolResult {
			t.Errorf("expected RoleToolResult, got %v", result.Role)
		}

		// Check for error content
		if len(result.Content) == 0 {
			t.Fatal("expected error result to have content")
		}
	})
}

// TestExecuteToolCall_ExecuteError tests tool execution errors.
func TestExecuteToolCall_ExecuteError(t *testing.T) {
	t.Run("wraps execution error in result message", func(t *testing.T) {
		ctx := context.Background()
		expectedErr := errors.New("tool failed")
		state := agent.NewAgentState(agent.AgentStateOptions{
			Tools: []agent.Tool{
				{
					Name: "failing_tool",
					Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
						return nil, expectedErr
					},
				},
			},
		})

		toolCall := agent.ToolCallContent{
			ID:        "call_123",
			Name:      "failing_tool",
			Arguments: map[string]any{},
		}

		result, err := agent.ExecuteToolCall(ctx, &state, toolCall)

		// Should return both an error result message AND an error
		if err == nil {
			t.Fatal("expected error from failing tool, got nil")
		}

		if result.Role != agent.RoleToolResult {
			t.Errorf("expected RoleToolResult, got %v", result.Role)
		}

		if !result.IsError {
			t.Error("expected IsError to be true for failed execution")
		}
	})
}

// TestFindToolByName tests finding tools by name.
func TestFindToolByName(t *testing.T) {
	t.Run("finds existing tool", func(t *testing.T) {
		state := agent.NewAgentState(agent.AgentStateOptions{
			Tools: []agent.Tool{
				{Name: "tool1", Description: "First tool"},
				{Name: "tool2", Description: "Second tool"},
			},
		})

		tool := agent.FindToolByName(&state, "tool2")

		if tool == nil {
			t.Fatal("expected to find tool2, got nil")
		}

		if tool.Name != "tool2" {
			t.Errorf("expected tool name 'tool2', got %q", tool.Name)
		}
	})

	t.Run("returns nil for unknown tool", func(t *testing.T) {
		state := agent.NewAgentState(agent.AgentStateOptions{
			Tools: []agent.Tool{
				{Name: "tool1"},
			},
		})

		tool := agent.FindToolByName(&state, "unknown")

		if tool != nil {
			t.Errorf("expected nil for unknown tool, got %v", tool)
		}
	})
}

// TestToolResultContentTests tests constructing tool result content.
func TestToolResultContentTests(t *testing.T) {
	t.Run("creates text content result", func(t *testing.T) {
		result := &agent.ToolResult{
			Content: []agent.ContentBlock{
				agent.TextContent{Text: "Success!"},
			},
		}

		if len(result.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(result.Content))
		}

		text, ok := result.Content[0].(agent.TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", result.Content[0])
		}

		if text.Text != "Success!" {
			t.Errorf("expected text 'Success!', got %q", text.Text)
		}
	})

	t.Run("creates content with details", func(t *testing.T) {
		details := map[string]any{"status": "completed"}
		result := &agent.ToolResult{
			Content: []agent.ContentBlock{
				agent.TextContent{Text: "Done"},
			},
			Details: details,
		}

		if result.Details == nil {
			t.Error("expected Details to be set")
		}
	})
}
