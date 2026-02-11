// Package agent provides tool execution framework for the agent runtime.
package agent

import (
	"context"
	"fmt"
	"time"
)

// ExecuteToolCall executes a tool call from the agent.
//
// It looks up the tool by name in the agent's state, calls the tool's Execute
// function with context, and returns a ToolResult as an AgentMessage with RoleToolResult.
func ExecuteToolCall(ctx context.Context, state *AgentState, toolCall ToolCallContent) (AgentMessage, error) {
	// Find the tool by name
	tool := FindToolByName(state, toolCall.Name)
	if tool == nil {
		err := &ToolExecutionError{
			ToolName: toolCall.Name,
			Message:  fmt.Sprintf("tool %q not found", toolCall.Name),
		}
		return errorResult(toolCall, err), err
	}

	// Check for nil Execute function
	if tool.Execute == nil {
		err := &ToolExecutionError{
			ToolName: toolCall.Name,
			Message:  "tool Execute function is nil",
		}
		return errorResult(toolCall, err), err
	}

	// Execute the tool
	result, err := tool.Execute(ctx, toolCall.Arguments)
	if err != nil {
		toolErr := &ToolExecutionError{
			ToolName: toolCall.Name,
			Message:  err.Error(),
		}
		return errorResult(toolCall, toolErr), toolErr
	}

	// Build result message
	content := result.Content
	if len(content) == 0 {
		content = []ContentBlock{TextContent{Text: ""}}
	}

	return AgentMessage{
		Role:       RoleToolResult,
		Content:    content,
		Timestamp:  time.Now().Unix(),
		ToolCallID: toolCall.ID,
		ToolName:   toolCall.Name,
		IsError:    false,
	}, nil
}

// errorResult creates an error result message for a tool call.
func errorResult(toolCall ToolCallContent, err error) AgentMessage {
	return AgentMessage{
		Role:       RoleToolResult,
		Content:    []ContentBlock{TextContent{Text: err.Error()}},
		Timestamp:  time.Now().Unix(),
		ToolCallID: toolCall.ID,
		ToolName:   toolCall.Name,
		IsError:    true,
	}
}

// FindToolByName locates a tool in the agent's tool list by name.
//
// Returns nil if the tool is not found.
func FindToolByName(state *AgentState, name string) *Tool {
	for i := range state.Tools {
		if state.Tools[i].Name == name {
			return &state.Tools[i]
		}
	}
	return nil
}
