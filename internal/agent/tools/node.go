// Package tools provides node tool integration with the agent.
//
// Node tools allow agents to execute tools on remote nodes (user devices,
// managed servers) via WebSocket connections. Tools are named using the
// convention: {platform}_{node_uuid}_{tool_name}[_variant]
//
// Example: mac_a1b2c3_bash, ios_d4e5f6_camera, managed_x1y2z3_bash
package tools

import (
	"context"
	"fmt"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/node"
)

// NodeToolExecutor is the interface for executing tools on nodes.
type NodeToolExecutor interface {
	ExecuteOnNode(ctx context.Context, nodeID, toolName string, args map[string]any) (*agent.ToolResult, error)
}

// NodeToolRegistry manages node tool registration and creates agent.Tool instances.
type NodeToolRegistry struct {
	executor NodeToolExecutor
	// nodeTools tracks which tools are available on which nodes
	// key: toolName (e.g., "mac_a1b2c3_bash"), value: nodeID
	nodeTools map[string]string
}

// NewNodeToolRegistry creates a new node tool registry.
func NewNodeToolRegistry(executor NodeToolExecutor) *NodeToolRegistry {
	return &NodeToolRegistry{
		executor:  executor,
		nodeTools: make(map[string]string),
	}
}

// RegisterNodeTools registers tools from a connected node.
//
// The nodeInfo contains the node's platform, ID, and list of available tools.
// This method generates full tool names (e.g., "mac_a1b2c3_bash") and tracks them.
func (r *NodeToolRegistry) RegisterNodeTools(nodeInfo node.NodeInfo) []agent.Tool {
	var tools []agent.Tool

	for _, toolName := range nodeInfo.Tools {
		// Generate full tool name with platform prefix
		fullToolName := node.GenerateToolName(nodeInfo.Platform, nodeInfo.NodeID, toolName, false)

		// Track which node this tool belongs to
		r.nodeTools[fullToolName] = nodeInfo.NodeID

		// Create agent tool that routes to node
		tools = append(tools, r.createNodeTool(fullToolName, nodeInfo.NodeID, toolName, false))

		// For user nodes (not managed), also add _onhost variant
		if nodeInfo.Platform != "managed" {
			onHostToolName := node.GenerateToolName(nodeInfo.Platform, nodeInfo.NodeID, toolName, true)
			r.nodeTools[onHostToolName] = nodeInfo.NodeID
			tools = append(tools, r.createNodeTool(onHostToolName, nodeInfo.NodeID, toolName+"_onhost", true))
		}
	}

	return tools
}

// UnregisterNodeTools removes all tools for a node.
func (r *NodeToolRegistry) UnregisterNodeTools(nodeInfo node.NodeInfo) {
	// Remove all tools that belong to this node
	for toolName := range r.nodeTools {
		if r.nodeTools[toolName] == nodeInfo.NodeID {
			delete(r.nodeTools, toolName)
		}
	}
}

// GetNodeForTool returns the node ID that hosts the given tool.
func (r *NodeToolRegistry) GetNodeForTool(toolName string) (string, bool) {
	nodeID, exists := r.nodeTools[toolName]
	return nodeID, exists
}

// IsNodeTool returns true if the tool is a node tool.
func (r *NodeToolRegistry) IsNodeTool(toolName string) bool {
	_, exists := r.nodeTools[toolName]
	return exists
}

// createNodeTool creates an agent.Tool that executes on a node.
func (r *NodeToolRegistry) createNodeTool(fullToolName, nodeID, baseToolName string, isOnHost bool) agent.Tool {
	return agent.Tool{
		Name:        fullToolName,
		Description: fmt.Sprintf("Execute %s on %s node (%s)", baseToolName, nodeID, map[bool]string{true: "host", false: "container"}[isOnHost]),
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			// Execute via node executor
			result, err := r.executor.ExecuteOnNode(ctx, nodeID, baseToolName, args)
			if err != nil {
				return nil, fmt.Errorf("node execution failed on %s: %w", nodeID, err)
			}
			return result, nil
		},
	}
}

// GetAllNodeTools returns all registered node tools as agent.Tools.
func (r *NodeToolRegistry) GetAllNodeTools() []agent.Tool {
	var tools []agent.Tool

	// This would need to track tool metadata to recreate
	// For now, return empty - tools should be recreated from node registrations
	return tools
}
