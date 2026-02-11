// Package agent provides mock implementations for testing the agent runtime.
package agent

import (
	"context"
	"time"
)

// MockLLMClient is a mock implementation of LLMClient for testing.
type MockLLMClient struct {
	Responses     []MockResponse
	responseIndex int
}

// MockResponse represents a canned response from the mock LLM.
type MockResponse struct {
	Message    AgentMessage
	BlockUntil chan struct{}
}

// LLMClient defines the interface for calling LLM APIs.
type LLMClient interface {
	// Stream sends messages to the LLM and returns a channel of events.
	Stream(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan AgentEvent, error)
}

// Stream implements LLMClient by returning canned responses.
func (m *MockLLMClient) Stream(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan AgentEvent, error) {
	if m.responseIndex >= len(m.Responses) {
		// Return an empty channel if no more responses
		ch := make(chan AgentEvent)
		close(ch)
		return ch, nil
	}

	resp := m.Responses[m.responseIndex]
	m.responseIndex++

	// If blocking is requested, wait until the channel is closed
	if resp.BlockUntil != nil {
		<-resp.BlockUntil
	}

	ch := make(chan AgentEvent, 10)

	go func() {
		defer close(ch)

		// Check context before sending events
		select {
		case <-ctx.Done():
			return
		default:
		}

		timestamp := time.Now().Unix()

		// Emit message start
		ch <- MessageStartEvent{
			Timestamp: timestamp,
			Message:   resp.Message,
		}

		// Extract text for delta if it's text content
		var delta string
		if len(resp.Message.Content) > 0 {
			if tc, ok := resp.Message.Content[0].(TextContent); ok {
				delta = tc.Text
			}
		}

		// Emit message update
		ch <- MessageUpdateEvent{
			Timestamp: timestamp,
			Message:   resp.Message,
			Delta:     delta,
		}

		// Emit message end
		ch <- MessageEndEvent{
			Timestamp: timestamp,
			Message:   resp.Message,
		}
	}()

	return ch, nil
}

// Reset resets the response index for reusing the mock.
func (m *MockLLMClient) Reset() {
	m.responseIndex = 0
}

// MockToolConfig configures a mock tool for testing.
type MockToolConfig struct {
	// Name is the tool name.
	Name string
	// Description is the tool description.
	Description string
	// Result is the canned result to return.
	Result *ToolResult
	// Error is the canned error to return. If set, Result is ignored.
	Error error
	// ExecuteFunc allows custom execution logic. If set, Result and Error are ignored.
	ExecuteFunc func(ctx context.Context, args map[string]any) (*ToolResult, error)
}

// MockTool creates a Tool for testing based on the provided config.
func MockTool(config MockToolConfig) Tool {
	if config.Name == "" {
		config.Name = "mock_tool"
	}

	var execute func(ctx context.Context, args map[string]any) (*ToolResult, error)

	if config.ExecuteFunc != nil {
		execute = config.ExecuteFunc
	} else if config.Error != nil {
		execute = func(ctx context.Context, args map[string]any) (*ToolResult, error) {
			return nil, config.Error
		}
	} else if config.Result != nil {
		execute = func(ctx context.Context, args map[string]any) (*ToolResult, error) {
			return config.Result, nil
		}
	} else {
		// Default: return empty result
		execute = func(ctx context.Context, args map[string]any) (*ToolResult, error) {
			return &ToolResult{
				Content: []ContentBlock{TextContent{Text: "ok"}},
			}, nil
		}
	}

	return Tool{
		Name:        config.Name,
		Description: config.Description,
		Execute:     execute,
	}
}

// MockEchoTool creates a tool that echoes back a string argument.
func MockEchoTool(name string) Tool {
	return MockTool(MockToolConfig{
		Name:        name,
		Description: "Echoes back the 'input' argument",
		ExecuteFunc: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
			input, ok := args["input"].(string)
			if !ok {
				return nil, &ToolExecutionError{
					ToolName: name,
					Message:  "missing or invalid 'input' argument",
				}
			}
			return &ToolResult{
				Content: []ContentBlock{TextContent{Text: input}},
			}, nil
		},
	})
}

// MockFailingTool creates a tool that always returns the given error.
func MockFailingTool(name string, err error) Tool {
	return MockTool(MockToolConfig{
		Name:        name,
		Description: "A tool that always fails",
		Error:       err,
	})
}

// MockResultTool creates a tool that returns the given result.
func MockResultTool(name string, result *ToolResult) Tool {
	return MockTool(MockToolConfig{
		Name:        name,
		Description: "A tool with a canned result",
		Result:      result,
	})
}
