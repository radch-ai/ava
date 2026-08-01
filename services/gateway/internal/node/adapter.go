// Package node provides adapter for integrating node tools with the agent.
package node

import (
	"context"
	"fmt"

	"github.com/radch-ai/ava/services/gateway/internal/agent"
)

// ToolAdapter bridges node tools with the agent's tool execution system.
// It intercepts tool calls for node tools and routes them to the appropriate node.
type ToolAdapter struct {
	executor *Executor
}

// NewToolAdapter creates a new node tool adapter.
func NewToolAdapter(executor *Executor) *ToolAdapter {
	return &ToolAdapter{
		executor: executor,
	}
}

// AdaptTool creates an agent.Tool that executes on a node.
// The tool name should be in the format: {platform}_{node_uuid}_{tool_name}
func (a *ToolAdapter) AdaptTool(toolName string) agent.Tool {
	return agent.Tool{
		Name:        toolName,
		Description: fmt.Sprintf("Execute %s on remote node", toolName),
		Execute:     a.createExecuteFunc(toolName),
	}
}

// createExecuteFunc creates an Execute function for a node tool.
func (a *ToolAdapter) createExecuteFunc(toolName string) func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
	return func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
		// Parse tool name to extract node ID and base tool name
		platform, nodeID, baseTool, isOnHost := ToolIdentifier(toolName)

		// If not a node tool, return error
		if platform == "" {
			return nil, fmt.Errorf("invalid node tool name: %s", toolName)
		}

		// For node tools, the base tool name is what the node recognizes
		// e.g., "mac_a1b2c3_bash" -> baseTool="bash"
		actualToolName := baseTool
		if isOnHost {
			actualToolName = baseTool + "_onhost"
		}

		// Execute on the node via executor
		result, err := a.executor.ExecuteOnNode(ctx, nodeID, actualToolName, args)
		if err != nil {
			return nil, fmt.Errorf("node execution failed: %w", err)
		}

		return result, nil
	}
}

// CreateNodeTools creates agent.Tool entries for all tools registered on nodes.
func (a *ToolAdapter) CreateNodeTools(nodeTools []string) []agent.Tool {
	tools := make([]agent.Tool, 0, len(nodeTools))
	for _, toolName := range nodeTools {
		// Only create tools for node tools (those with platform prefix)
		if IsNodeTool(toolName) {
			tools = append(tools, a.AdaptTool(toolName))
		}
	}
	return tools
}

// GetUserNodeTools returns all node tools available to a user.
// It combines tools from all nodes registered for the user.
func (a *ToolAdapter) GetUserNodeTools(ctx context.Context, registry NodeRegistry, userID string) []string {
	var toolNames []string

	nodes := registry.GetUserNodes(userID)
	for _, node := range nodes {
		for _, tool := range node.Tools {
			// Generate full tool name with platform prefix
			fullToolName := GenerateToolName(node.Platform, node.NodeID, tool, false)
			toolNames = append(toolNames, fullToolName)

			// Also add _onhost variant for user nodes
			if node.Platform != "managed" {
				onHostToolName := GenerateToolName(node.Platform, node.NodeID, tool, true)
				toolNames = append(toolNames, onHostToolName)
			}
		}
	}

	return toolNames
}

// NodeRegistry is the interface needed by the tool adapter to access node information.
type NodeRegistry interface {
	GetUserNodes(userID string) []NodeInfo
}
