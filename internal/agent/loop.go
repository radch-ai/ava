// Package agent provides the agent loop for tool calling and message processing.
package agent

import (
	"context"
	"errors"
	"fmt"
)

// ErrSteered is returned when the loop is steered by an external signal.
var ErrSteered = errors.New("loop steered")

// ToolExecutionResult represents the result of executing a tool.
type ToolExecutionResult struct {
	ToolCallID string
	ToolName   string
	Result     *ToolResult
	Error      error
	IsError    bool
}

// Loop manages the agent execution loop.
type Loop struct {
	client LLMClient
	tools  []Tool
}

// NewLoop creates a new Loop with the given LLM client.
func NewLoop(client LLMClient) *Loop {
	return &Loop{client: client}
}

// RunResult is the result of running the loop.
type RunResult struct {
	Messages []AgentMessage
}

// Run executes the agent loop starting with the given messages.
func (l *Loop) Run(ctx context.Context, messages []AgentMessage, tools ...Tool) (*RunResult, error) {
	l.tools = tools
	currentMessages := make([]AgentMessage, len(messages))
	copy(currentMessages, messages)

	for {
		// Check context
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Stream from LLM
		eventCh, err := l.client.Stream(ctx, currentMessages, l.tools)
		if err != nil {
			return nil, fmt.Errorf("LLM stream failed: %w", err)
		}

		// Collect the final message from events
		var assistantMsg *AgentMessage
		for event := range eventCh {
			switch e := event.(type) {
			case MessageEndEvent:
				assistantMsg = &e.Message
			case TurnStartEvent, TurnEndEvent, MessageStartEvent, MessageUpdateEvent:
				// Ignore other events for now
			default:
				// Ignore unknown events
			}
		}

		if assistantMsg == nil {
			// No message received, exit
			break
		}

		// Add assistant message to history
		currentMessages = append(currentMessages, *assistantMsg)

		// Extract tool calls
		toolCalls := l.ExtractToolCalls(*assistantMsg)

		// If no tool calls, we're done
		if len(toolCalls) == 0 {
			break
		}

		// Execute tools
		results := l.executeTools(ctx, toolCalls)

		// Add tool results to history
		currentMessages = l.AddToolResults(currentMessages, results)
	}

	return &RunResult{
		Messages: currentMessages,
	}, nil
}

// RunWithSteer executes the agent loop with steer channel checking at tool boundaries.
func (l *Loop) RunWithSteer(ctx context.Context, messages []AgentMessage, tools []Tool, lane *Lane, steerCh <-chan struct{}) (*RunResult, error) {
	l.tools = tools
	currentMessages := make([]AgentMessage, len(messages))
	copy(currentMessages, messages)

	for {
		// Check context
		select {
		case <-ctx.Done():
			return nil, ErrSteered
		case <-steerCh:
			return nil, ErrSteered
		default:
		}

		// Stream from LLM
		eventCh, err := l.client.Stream(ctx, currentMessages, l.tools)
		if err != nil {
			return nil, fmt.Errorf("LLM stream failed: %w", err)
		}

		// Collect the final message from events
		var assistantMsg *AgentMessage
		for event := range eventCh {
			switch e := event.(type) {
			case MessageEndEvent:
				assistantMsg = &e.Message
			case TurnStartEvent, TurnEndEvent, MessageStartEvent, MessageUpdateEvent:
				// Ignore other events for now
			default:
				// Ignore unknown events
			}
		}

		if assistantMsg == nil {
			// No message received, exit
			break
		}

		// Add assistant message to history
		currentMessages = append(currentMessages, *assistantMsg)

		// Extract tool calls
		toolCalls := l.ExtractToolCalls(*assistantMsg)

		// If no tool calls, we're done
		if len(toolCalls) == 0 {
			break
		}

		// Check steer channel again before tool execution
		select {
		case <-ctx.Done():
			return nil, ErrSteered
		case <-steerCh:
			return nil, ErrSteered
		default:
		}

		// Drain pending messages if any
		drainedMessages := lane.DrainPending()
		if len(drainedMessages) > 0 {
			currentMessages = append(currentMessages, drainedMessages...)
			continue
		}

		// Execute tools
		results := l.executeTools(ctx, toolCalls)

		// Add tool results to history
		currentMessages = l.AddToolResults(currentMessages, results)
	}

	return &RunResult{
		Messages: currentMessages,
	}, nil
}

// ExtractToolCalls extracts tool calls from an assistant message.
func (l *Loop) ExtractToolCalls(msg AgentMessage) []ToolCallContent {
	// Only extract from assistant messages
	if msg.Role != RoleAssistant {
		return nil
	}

	var calls []ToolCallContent
	for _, block := range msg.Content {
		if tc, ok := block.(ToolCallContent); ok {
			calls = append(calls, tc)
		}
	}
	return calls
}

// AddToolResults adds tool result messages to the message history.
func (l *Loop) AddToolResults(messages []AgentMessage, results []ToolExecutionResult) []AgentMessage {
	updated := make([]AgentMessage, len(messages))
	copy(updated, messages)

	for _, result := range results {
		var resultMsg AgentMessage
		if result.IsError {
			// Create error result message
			text := ""
			if result.Error != nil {
				text = result.Error.Error()
			}
			resultMsg = NewToolResultMessage(result.ToolCallID, result.ToolName, text, true)
		} else if result.Result != nil && len(result.Result.Content) > 0 {
			// Extract text from result content
			text := ""
			if tc, ok := result.Result.Content[0].(TextContent); ok {
				text = tc.Text
			}
			resultMsg = NewToolResultMessage(result.ToolCallID, result.ToolName, text, false)
		} else {
			// Empty result
			resultMsg = NewToolResultMessage(result.ToolCallID, result.ToolName, "", false)
		}
		updated = append(updated, resultMsg)
	}

	return updated
}

// executeTools executes the given tool calls and returns their results.
func (l *Loop) executeTools(ctx context.Context, toolCalls []ToolCallContent) []ToolExecutionResult {
	results := make([]ToolExecutionResult, 0, len(toolCalls))

	for _, tc := range toolCalls {
		// Find tool by name
		var tool *Tool
		for i := range l.tools {
			if l.tools[i].Name == tc.Name {
				tool = &l.tools[i]
				break
			}
		}

		result := ToolExecutionResult{
			ToolCallID: tc.ID,
			ToolName:   tc.Name,
		}

		if tool == nil {
			result.IsError = true
			result.Error = &ToolExecutionError{
				ToolName: tc.Name,
				Message:  fmt.Sprintf("tool %q not found", tc.Name),
			}
		} else if tool.Execute == nil {
			result.IsError = true
			result.Error = &ToolExecutionError{
				ToolName: tc.Name,
				Message:  "tool Execute function is nil",
			}
		} else {
			// Execute tool
			toolResult, err := tool.Execute(ctx, tc.Arguments)
			if err != nil {
				result.IsError = true
				result.Error = &ToolExecutionError{
					ToolName: tc.Name,
					Message:  err.Error(),
				}
			} else {
				result.Result = toolResult
			}
		}

		results = append(results, result)
	}

	return results
}
