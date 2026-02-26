// Package agent_test provides tests for the agent runtime loop.
package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

func TestLoopWithNoToolCalls(t *testing.T) {
	t.Run("exits after text response without looping", func(t *testing.T) {
		ctx := context.Background()

		// Mock LLM returns text-only response - no tool calls
		mock := &agent.MockLLMClient{
			Responses: []agent.MockResponse{
				{
					// Text response with no tool calls means loop exits
					Message: agent.AgentMessage{
						Role:      agent.RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content:   []agent.ContentBlock{agent.TextContent{Text: "Hello, world!"}},
					},
				},
			},
		}

		loop := agent.NewLoop(mock)
		initialMessages := []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Hi there"),
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
		if finalMsg.Role != agent.RoleAssistant {
			t.Errorf("expected final message role %v, got %v", agent.RoleAssistant, finalMsg.Role)
		}
	})
}

func TestLoopWithToolCall(t *testing.T) {
	t.Run("executes tool and continues with result", func(t *testing.T) {
		ctx := context.Background()

		// Mock LLM returns a tool call
		mock := &agent.MockLLMClient{
			Responses: []agent.MockResponse{
				{
					Message: agent.AgentMessage{
						Role:      agent.RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content: []agent.ContentBlock{
							agent.ToolCallContent{
								ID:   "call_123",
								Name: "echo",
								Arguments: map[string]any{
									"input": "hello",
								},
							},
						},
					},
				},
				{
					// After tool result, LLM responds with text
					Message: agent.AgentMessage{
						Role:      agent.RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content:   []agent.ContentBlock{agent.TextContent{Text: "Done!"}},
					},
				},
			},
		}

		loop := agent.NewLoop(mock)

		// Provide echo tool
		echoTool := agent.MockEchoTool("echo")

		initialMessages := []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Use echo tool"),
		}

		result, err := loop.Run(ctx, initialMessages, echoTool)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// Should have: user, tool call, tool result, final response
		if len(result.Messages) != 4 {
			t.Errorf("expected 4 messages, got %d", len(result.Messages))
		}

		// Check tool result message
		toolResultMsg := result.Messages[2]
		if toolResultMsg.Role != agent.RoleToolResult {
			t.Errorf("expected tool result role, got %v", toolResultMsg.Role)
		}
	})
}

func TestLoopWithMultipleToolCalls(t *testing.T) {
	t.Run("executes multiple tools in sequence", func(t *testing.T) {
		ctx := context.Background()

		mock := &agent.MockLLMClient{
			Responses: []agent.MockResponse{
				{
					// Response with two tool calls
					Message: agent.AgentMessage{
						Role:      agent.RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content: []agent.ContentBlock{
							agent.ToolCallContent{
								ID:   "call_1",
								Name: "echo",
								Arguments: map[string]any{
									"input": "first",
								},
							},
							agent.ToolCallContent{
								ID:   "call_2",
								Name: "echo",
								Arguments: map[string]any{
									"input": "second",
								},
							},
						},
					},
				},
				{
					// Final response after both tools
					Message: agent.AgentMessage{
						Role:      agent.RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content:   []agent.ContentBlock{agent.TextContent{Text: "Both done!"}},
					},
				},
			},
		}

		loop := agent.NewLoop(mock)
		echoTool := agent.MockEchoTool("echo")

		initialMessages := []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Use echo twice"),
		}

		result, err := loop.Run(ctx, initialMessages, echoTool)

		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		// user + tool calls + 2 tool results + final response
		if len(result.Messages) != 5 {
			t.Errorf("expected 5 messages, got %d", len(result.Messages))
		}
	})
}

func TestLoopToolExecutionError(t *testing.T) {
	t.Run("handles tool errors gracefully", func(t *testing.T) {
		ctx := context.Background()

		expectedErr := errors.New("tool execution failed")

		mock := &agent.MockLLMClient{
			Responses: []agent.MockResponse{
				{
					Message: agent.AgentMessage{
						Role:      agent.RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content: []agent.ContentBlock{
							agent.ToolCallContent{
								ID:        "call_error",
								Name:      "failing_tool",
								Arguments: map[string]any{},
							},
						},
					},
				},
				{
					// LLM acknowledges error
					Message: agent.AgentMessage{
						Role:      agent.RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content:   []agent.ContentBlock{agent.TextContent{Text: "Sorry about that"}},
					},
				},
			},
		}

		loop := agent.NewLoop(mock)
		failingTool := agent.MockFailingTool("failing_tool", expectedErr)

		initialMessages := []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Trigger error"),
		}

		result, err := loop.Run(ctx, initialMessages, failingTool)

		// Loop should still complete despite tool error
		if err != nil {
			t.Fatalf("expected no error from loop, got %v", err)
		}

		// Verify tool result contains error
		toolResultMsg := result.Messages[2]
		if toolResultMsg.Role != agent.RoleToolResult {
			t.Errorf("expected tool result role, got %v", toolResultMsg.Role)
		}
	})
}

func TestLoop_SteerAtToolBoundary(t *testing.T) {
	t.Run("stops loop on steerCh signal", func(t *testing.T) {
		ctx := context.Background()

		// Mock LLM returns a tool call
		mock := &agent.MockLLMClient{
			Responses: []agent.MockResponse{
				{
					Message: agent.AgentMessage{
						Role:      agent.RoleAssistant,
						Timestamp: time.Now().Unix(),
						Content: []agent.ContentBlock{
							agent.ToolCallContent{
								ID:   "call_123",
								Name: "echo",
								Arguments: map[string]any{
									"input": "test",
								},
							},
						},
					},
				},
			},
		}

		loop := agent.NewLoop(mock)
		echoTool := agent.MockEchoTool("echo")

		initialMessages := []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Use echo tool"),
		}

		// Create lane and steer channel
		lane := agent.NewLane()
		steerCh := make(chan struct{})

		// Start the goroutine to signal steer after a short delay
		go func() {
			time.Sleep(10 * time.Millisecond)
			close(steerCh)
		}()

		// RunWithSteer should return ErrSteered
		result, err := loop.RunWithSteer(ctx, initialMessages, []agent.Tool{echoTool}, lane, steerCh)

		// Should return ErrSteered
		if err != agent.ErrSteered {
			t.Errorf("expected ErrSteered, got %v", err)
		}

		// Result should be nil when steered
		if result != nil {
			t.Errorf("expected nil result, got %v", result)
		}
	})
}

func TestExtractToolCalls(t *testing.T) {
	t.Run("extracts tool calls from message", func(t *testing.T) {
		mock := &agent.MockLLMClient{}
		loop := agent.NewLoop(mock)

		msg := agent.AgentMessage{
			Role: agent.RoleAssistant,
			Content: []agent.ContentBlock{
				agent.TextContent{Text: "I'll help"},
				agent.ToolCallContent{
					ID:        "call_1",
					Name:      "tool1",
					Arguments: map[string]any{"arg": "val"},
				},
			},
		}

		calls := loop.ExtractToolCalls(msg)

		if len(calls) != 1 {
			t.Errorf("expected 1 tool call, got %d", len(calls))
		}

		if calls[0].Name != "tool1" {
			t.Errorf("expected tool name 'tool1', got %q", calls[0].Name)
		}
	})

	t.Run("returns empty slice for message without tool calls", func(t *testing.T) {
		mock := &agent.MockLLMClient{}
		loop := agent.NewLoop(mock)

		msg := agent.AgentMessage{
			Role:    agent.RoleAssistant,
			Content: []agent.ContentBlock{agent.TextContent{Text: "Just text"}},
		}

		calls := loop.ExtractToolCalls(msg)

		if len(calls) != 0 {
			t.Errorf("expected 0 tool calls, got %d", len(calls))
		}
	})
}
