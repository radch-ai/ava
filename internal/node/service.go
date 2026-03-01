// Package node provides the node service that bridges node connections with agents.
package node

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/rs/zerolog"
)

// Service manages node connections and integrates them with agents.
type Service struct {
	registry      *Registry
	executor      *Executor
	agentProvider AgentProvider
	logger        *zerolog.Logger
}

// AgentProvider is an interface for getting agents by session/user.
type AgentProvider interface {
	GetAgentForUser(userID string) (*agent.Agent, error)
}

// NewService creates a new node service.
func NewService(logger *zerolog.Logger, agentProvider AgentProvider) *Service {
	registry := NewRegistry()
	executor := NewExecutor(ExecutorOptions{
		Registry: registry,
		Timeout:  30 * time.Second,
		Logger:   *logger,
	})

	return &Service{
		registry:      registry,
		executor:      executor,
		agentProvider: agentProvider,
		logger:        logger,
	}
}

// RegisterConnection registers a node connection and adds its tools to the user's agent.
func (s *Service) RegisterConnection(ctx context.Context, conn WebSocketConn, nodeID, userID, platform string, toolSpecs []ToolSpec) error {
	// Convert tool specs to string list for registry
	toolNames := make([]string, 0, len(toolSpecs))
	for _, spec := range toolSpecs {
		toolNames = append(toolNames, spec.Name)
	}

	// 1. Register the node in the registry
	err := s.registry.RegisterNode(ctx, userID, nodeID, platform, toolNames, conn)
	if err != nil {
		return fmt.Errorf("failed to register node: %w", err)
	}

	// 2. Create NodeConnection for executor
	nodeConn := &NodeConnection{
		NodeUUID: nodeID,
		Conn:     conn,
		SendCh:   make(chan []byte, 10),
	}
	s.registry.RegisterConnection(nodeConn)

	// 3. Create agent tools for this node
	nodeInfo := NodeInfo{
		NodeID:   nodeID,
		Platform: platform,
		UserID:   userID,
		Tools:    toolNames,
		Status:   NodeStatusOnline,
	}

	// 4. Get the user's agent and add tools
	agent, err := s.agentProvider.GetAgentForUser(userID)
	if err != nil {
		return fmt.Errorf("failed to get agent for user: %w", err)
	}

	// 5. Create tool instances for each node tool (respecting isolation modes)
	agentTools := s.createNodeToolsFromSpecs(nodeInfo, toolSpecs)

	// 6. Add tools to the agent
	_ = agent.AddTools(agentTools...)

	s.logger.Info().
		Str("node_id", nodeID).
		Str("user_id", userID).
		Str("platform", platform).
		Int("tool_count", len(agentTools)).
		Msg("Node registered and tools added to agent")

	// 7. Start listening for results from this node
	go s.executor.listenForResults(context.Background(), nodeConn)

	return nil
}

// UnregisterConnection removes a node connection and its tools from the user's agent.
func (s *Service) UnregisterConnection(ctx context.Context, nodeID, userID string) error {
	// 1. Get node info before unregistering
	nodeInfo := s.registry.GetNode(nodeID)
	if nodeInfo == nil {
		return nil // Already gone
	}

	// 2. Remove tools from agent with the node's prefix
	agent, err := s.agentProvider.GetAgentForUser(userID)
	if err == nil {
		// Remove all tools starting with platform_nodeID_ prefix
		prefix := fmt.Sprintf("%s_%s_", nodeInfo.Platform, nodeID)
		_ = agent.RemoveTools(prefix)
	}

	// 3. Unregister from registry
	err = s.registry.UnregisterNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("failed to unregister node: %w", err)
	}

	s.logger.Info().
		Str("node_id", nodeID).
		Str("user_id", userID).
		Msg("Node unregistered and tools removed from agent")

	return nil
}

// UpdateNodeHealth updates a node's health status.
func (s *Service) UpdateNodeHealth(nodeID string, status NodeStatus, toolSpecs []ToolSpec) error {
	// Convert tool specs to string list for registry
	toolNames := make([]string, 0, len(toolSpecs))
	for _, spec := range toolSpecs {
		toolNames = append(toolNames, spec.Name)
	}
	return s.registry.UpdateNodeHealth(nodeID, status, toolNames)
}

// ExecuteOnNode executes a tool on a specific node.
func (s *Service) ExecuteOnNode(ctx context.Context, nodeID, toolName string, args map[string]any) (*agent.ToolResult, error) {
	return s.executor.ExecuteOnNode(ctx, nodeID, toolName, args)
}

// createNodeToolsFromSpecs creates agent.Tool instances from tool specifications.
func (s *Service) createNodeToolsFromSpecs(nodeInfo NodeInfo, toolSpecs []ToolSpec) []agent.Tool {
	var tools []agent.Tool

	for _, spec := range toolSpecs {
		// Create a tool for each isolation mode the node supports
		for _, isolation := range spec.Isolation {
			var fullToolName string
			var isOnHost bool

			switch isolation {
			case "docker":
				fullToolName = GenerateToolName(nodeInfo.Platform, nodeInfo.NodeID, spec.Name, false)
				isOnHost = false
			case "host":
				fullToolName = GenerateToolName(nodeInfo.Platform, nodeInfo.NodeID, spec.Name, true)
				isOnHost = true
			default:
				// Unknown isolation mode, skip
				continue
			}

			tools = append(tools, s.createNodeTool(fullToolName, nodeInfo, spec.Name, isOnHost))
		}
	}

	return tools
}

// createNodeTool creates a single agent.Tool that executes on a node.
func (s *Service) createNodeTool(fullToolName string, nodeInfo NodeInfo, baseToolName string, isOnHost bool) agent.Tool {
	// Create detailed descriptions for the LLM to understand:
	// 1. Multiple tools may exist with same base name (different nodes)
	// 2. node_id identifies which specific device/server
	// 3. Related commands (same node_id) run on same device
	// 4. Isolation: Docker isolated (preferred) vs direct host (only if user requests)

	// Build platform display
	platformDisplay := capitalize(nodeInfo.Platform)

	// Build isolation description
	var isolation string
	if isOnHost {
		isolation = "runs directly on host, only use if user explicitly requests onhost or bypassing isolation"
	} else {
		isolation = "runs in Docker container, preferred option for safety"
	}

	// Example: "Execute bash. Node: Mac_abc123. Isolation: runs in Docker container, preferred option for safety."
	description := fmt.Sprintf("Execute %s. Node: %s_%s. Isolation: %s.",
		baseToolName, platformDisplay, nodeInfo.NodeID, isolation)

	// Capture isOnHost for the execute function
	capturedIsOnHost := isOnHost

	var executeFunc func(context.Context, string, string, map[string]any) (*agent.ToolResult, error)
	if capturedIsOnHost {
		executeFunc = s.executor.ExecuteOnNodeWithoutIsolation
	} else {
		executeFunc = s.executor.ExecuteOnNode
	}

	return agent.Tool{
		Name:        fullToolName,
		Description: description,
		Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
			result, err := executeFunc(ctx, nodeInfo.NodeID, baseToolName, args)
			if err != nil {
				return nil, fmt.Errorf("node execution failed: %w", err)
			}
			return result, nil
		},
	}
}

// capitalize capitalizes the first letter of a string.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
