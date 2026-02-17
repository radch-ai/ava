// Package node provides protocol types and utilities for distributed node execution.
package node

import (
	"strings"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// MessageType represents the type of message sent between node and gateway.
type MessageType string

const (
	MessageTypeRegister       MessageType = "register"
	MessageTypeHealthResponse MessageType = "health_response"
	MessageTypeExecute        MessageType = "execute"
	MessageTypeToolResult     MessageType = "tool_result"
	MessageTypeError          MessageType = "error"
)

func (mt MessageType) String() string {
	return string(mt)
}

// RegisterMessage is sent by a node when it connects.
type RegisterMessage struct {
	NodeID   string     `json:"node_id"`
	Platform string     `json:"platform"`
	Tools    []ToolSpec `json:"tools"`
}

// ToolSpec describes a tool available on a node.
type ToolSpec struct {
	Name      string   `json:"name"`
	Isolation []string `json:"isolation"` // ["docker"], ["docker", "host"], etc.
}

// HealthResponseMessage is sent by a node in response to health checks.
type HealthResponseMessage struct {
	NodeID string     `json:"node_id"`
	Tools  []ToolSpec `json:"tools"`
	Status string     `json:"status"`
}

// ExecuteMessage is sent by the gateway to request tool execution.
type ExecuteMessage struct {
	ToolName  string                 `json:"tool"`
	Arguments map[string]interface{} `json:"args"`
	CallID    string                 `json:"call_id"`
	Isolation string                 `json:"isolation,omitempty"` // "docker" or "host"
}

// ToolResultMessage is sent by a node with execution results.
type ToolResultMessage struct {
	CallID string            `json:"call_id"`
	Result *agent.ToolResult `json:"result,omitempty"`
	Error  *string           `json:"error,omitempty"`
}

// ContentBlock represents a block of content in a tool result.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolResult is an alias for agent.ToolResult.
type ToolResult = agent.ToolResult

// NodeConnection represents a connected node with its WebSocket connection.
type NodeConnection struct {
	NodeUUID string
	Conn     WebSocketConn
	SendCh   chan []byte
}

// WebSocketConn is an interface for WebSocket-like connections.
type WebSocketConn interface {
	WriteMessage(messageType int, data []byte) error
	ReadMessage() (int, []byte, error)
	Close() error
}

// ErrorMessage is sent for protocol-level errors.
type ErrorMessage struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// NodeInfo represents a connected node.
type NodeInfo struct {
	NodeID      string
	Platform    string
	UserID      string
	ConnectedAt time.Time
	LastSeenAt  time.Time
	LastSeen    time.Time // Alias for LastSeenAt for handler test compatibility
	Tools       []string
	Status      NodeStatus
}

// NodeStatus represents the health status of a node.
type NodeStatus string

const (
	NodeStatusOnline    NodeStatus = "online"
	NodeStatusBusy      NodeStatus = "busy"
	NodeStatusHealthy   NodeStatus = "healthy"
	NodeStatusDegraded  NodeStatus = "degraded"
	NodeStatusUnhealthy NodeStatus = "unhealthy"
	NodeStatusOffline   NodeStatus = "offline"
)

func (ns NodeStatus) String() string {
	return string(ns)
}

func ToolIdentifier(toolName string) (platform, nodeID, baseTool string, isOnHost bool) {
	if !containsUnderscorePrefix(toolName) {
		return "", "", toolName, false
	}
	parts := splitToolName(toolName)
	if len(parts) < 3 {
		return "", "", toolName, false
	}
	platform = parts[0]
	nodeID = parts[1]
	baseTool = parts[2]
	isOnHost = len(parts) > 3 && parts[3] == "onhost"
	return platform, nodeID, baseTool, isOnHost
}

func IsNodeTool(toolName string) bool {
	platform, _, _, _ := ToolIdentifier(toolName)
	return platform != ""
}

func GenerateToolName(platform, nodeID, toolName string, isOnHost bool) string {
	name := platform + "_" + nodeID + "_" + toolName
	if isOnHost {
		name += "_onhost"
	}
	return name
}

func containsUnderscorePrefix(toolName string) bool {
	if toolName == "" {
		return false
	}
	knownPlatforms := []string{"mac_", "windows_", "linux_", "ios_", "android_", "managed_"}
	for _, prefix := range knownPlatforms {
		if strings.HasPrefix(toolName, prefix) {
			return true
		}
	}
	return false
}

func splitToolName(toolName string) []string {
	return strings.Split(toolName, "_")
}
