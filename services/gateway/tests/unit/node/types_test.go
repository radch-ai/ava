// Package node_test provides unit tests for node types and adapter.
package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTypesWebSocketConn is a mock WebSocket connection for testing.
type mockTypesWebSocketConn struct{}

func (m *mockTypesWebSocketConn) Write(ctx context.Context, typ coderws.MessageType, p []byte) error {
	return nil
}

func (m *mockTypesWebSocketConn) Reader(ctx context.Context) (coderws.MessageType, io.Reader, error) {
	return 0, nil, errors.New("not implemented")
}

func (m *mockTypesWebSocketConn) CloseNow() error {
	return nil
}

// TestMessageType_String tests MessageType string representation.
func TestMessageType_String(t *testing.T) {
	t.Run("returns correct string for all message types", func(t *testing.T) {
		tests := []struct {
			mt       node.MessageType
			expected string
		}{
			{node.MessageTypeRegister, "register"},
			{node.MessageTypeHealthResponse, "health_response"},
			{node.MessageTypeExecute, "execute"},
			{node.MessageTypeToolResult, "tool_result"},
			{node.MessageTypeError, "error"},
		}

		for _, tt := range tests {
			assert.Equal(t, tt.expected, tt.mt.String())
		}
	})
}

// TestToolSpec_MarshalUnmarshal tests ToolSpec JSON serialization.
func TestToolSpec_MarshalUnmarshal(t *testing.T) {
	t.Run("marshals and unmarshals tool spec with isolation", func(t *testing.T) {
		spec := node.ToolSpec{
			Name:      "bash",
			Isolation: []string{"docker", "host"},
		}

		data, err := json.Marshal(spec)
		require.NoError(t, err)

		var unmarshaled node.ToolSpec
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, "bash", unmarshaled.Name)
		assert.Equal(t, []string{"docker", "host"}, unmarshaled.Isolation)
	})

	t.Run("marshals and unmarshals tool spec without isolation", func(t *testing.T) {
		spec := node.ToolSpec{
			Name:      "python",
			Isolation: []string{},
		}

		data, err := json.Marshal(spec)
		require.NoError(t, err)

		var unmarshaled node.ToolSpec
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, "python", unmarshaled.Name)
		assert.Empty(t, unmarshaled.Isolation)
	})
}

// TestRegisterMessage tests RegisterMessage structure.
func TestRegisterMessage(t *testing.T) {
	t.Run("creates valid register message", func(t *testing.T) {
		msg := node.RegisterMessage{
			NodeID:   "node-123",
			Platform: "linux",
			Tools: []node.ToolSpec{
				{Name: "bash", Isolation: []string{"docker", "host"}},
				{Name: "python", Isolation: []string{"docker"}},
			},
		}

		assert.Equal(t, "node-123", msg.NodeID)
		assert.Equal(t, "linux", msg.Platform)
		assert.Len(t, msg.Tools, 2)
		assert.Equal(t, "bash", msg.Tools[0].Name)
		assert.Equal(t, "python", msg.Tools[1].Name)
	})
}

// TestHealthResponseMessage tests HealthResponseMessage structure.
func TestHealthResponseMessage(t *testing.T) {
	t.Run("creates valid health response message", func(t *testing.T) {
		msg := node.HealthResponseMessage{
			NodeID: "node-123",
			Tools: []node.ToolSpec{
				{Name: "bash", Isolation: []string{"docker"}},
			},
			Status: "healthy",
		}

		assert.Equal(t, "node-123", msg.NodeID)
		assert.Equal(t, "healthy", msg.Status)
		assert.Len(t, msg.Tools, 1)
	})
}

// TestExecuteMessage_Serialization tests ExecuteMessage JSON serialization.
func TestExecuteMessage_Serialization(t *testing.T) {
	t.Run("serializes execute message with isolation", func(t *testing.T) {
		msg := node.ExecuteMessage{
			ToolName:  "bash",
			Arguments: map[string]interface{}{"command": "ls -la"},
			CallID:    "call-123",
			Isolation: "docker",
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var unmarshaled node.ExecuteMessage
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, "bash", unmarshaled.ToolName)
		assert.Equal(t, "call-123", unmarshaled.CallID)
		assert.Equal(t, "docker", unmarshaled.Isolation)
		assert.Equal(t, "ls -la", unmarshaled.Arguments["command"])
	})

	t.Run("serializes execute message without isolation", func(t *testing.T) {
		msg := node.ExecuteMessage{
			ToolName:  "python",
			Arguments: map[string]interface{}{"script": "print('hello')"},
			CallID:    "call-456",
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var unmarshaled node.ExecuteMessage
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, "python", unmarshaled.ToolName)
		assert.Equal(t, "call-456", unmarshaled.CallID)
		assert.Empty(t, unmarshaled.Isolation)
	})

	t.Run("handles complex arguments", func(t *testing.T) {
		args := map[string]interface{}{
			"command": "run",
			"flags":   []string{"-v", "--debug"},
			"env": map[string]string{
				"PATH": "/usr/bin",
				"HOME": "/root",
			},
		}

		msg := node.ExecuteMessage{
			ToolName:  "docker",
			Arguments: args,
			CallID:    "call-789",
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var unmarshaled node.ExecuteMessage
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, "run", unmarshaled.Arguments["command"])
		flags := unmarshaled.Arguments["flags"].([]interface{})
		assert.Equal(t, "-v", flags[0])
	})
}

// TestToolResultMessage tests ToolResultMessage structure.
func TestToolResultMessage(t *testing.T) {
	t.Run("creates result message with success result", func(t *testing.T) {
		result := &agent.ToolResult{
			Content: []agent.ContentBlock{agent.TextContent{Text: "success"}},
		}
		errMsg := "error"

		msg := node.ToolResultMessage{
			CallID: "call-123",
			Result: result,
			Error:  &errMsg,
		}

		assert.Equal(t, "call-123", msg.CallID)
		assert.NotNil(t, msg.Result)
		assert.NotNil(t, msg.Error)
		assert.Equal(t, "error", *msg.Error)
	})

	t.Run("creates result message without error", func(t *testing.T) {
		result := &agent.ToolResult{
			Content: []agent.ContentBlock{agent.TextContent{Text: "output"}},
		}

		msg := node.ToolResultMessage{
			CallID: "call-456",
			Result: result,
			Error:  nil,
		}

		assert.Equal(t, "call-456", msg.CallID)
		assert.NotNil(t, msg.Result)
		assert.Nil(t, msg.Error)
	})
}

// TestNodeStatus tests NodeStatus enum.
func TestNodeStatus(t *testing.T) {
	t.Run("String returns correct status", func(t *testing.T) {
		tests := []struct {
			status   node.NodeStatus
			expected string
		}{
			{node.NodeStatusOnline, "online"},
			{node.NodeStatusBusy, "busy"},
			{node.NodeStatusHealthy, "healthy"},
			{node.NodeStatusDegraded, "degraded"},
			{node.NodeStatusUnhealthy, "unhealthy"},
			{node.NodeStatusOffline, "offline"},
		}

		for _, tt := range tests {
			assert.Equal(t, tt.expected, tt.status.String())
		}
	})
}

// TestNodeInfo tests NodeInfo structure and initialization.
func TestNodeInfo(t *testing.T) {
	t.Run("creates valid node info", func(t *testing.T) {
		now := time.Now()
		info := node.NodeInfo{
			NodeID:      "node-123",
			Platform:    "linux",
			UserID:      "user-456",
			ConnectedAt: now,
			LastSeenAt:  now,
			LastSeen:    now,
			Tools:       []string{"bash", "python"},
			Status:      node.NodeStatusOnline,
		}

		assert.Equal(t, "node-123", info.NodeID)
		assert.Equal(t, "linux", info.Platform)
		assert.Equal(t, "user-456", info.UserID)
		assert.Equal(t, now, info.ConnectedAt)
		assert.Equal(t, now, info.LastSeenAt)
		assert.Equal(t, now, info.LastSeen)
		assert.Equal(t, []string{"bash", "python"}, info.Tools)
		assert.Equal(t, node.NodeStatusOnline, info.Status)
	})

	t.Run("zero values for new node info", func(t *testing.T) {
		info := node.NodeInfo{}

		assert.Empty(t, info.NodeID)
		assert.Empty(t, info.Platform)
		assert.Empty(t, info.UserID)
		assert.True(t, info.ConnectedAt.IsZero())
		assert.True(t, info.LastSeenAt.IsZero())
		assert.Nil(t, info.Tools)
		assert.Empty(t, info.Status)
	})
}

// TestNodeConnection tests NodeConnection structure.
func TestNodeConnection(t *testing.T) {
	t.Run("creates valid node connection", func(t *testing.T) {
		conn := &mockTypesWebSocketConn{}
		sendCh := make(chan []byte, 10)

		nodeConn := node.NodeConnection{
			NodeUUID: "node-123",
			Conn:     conn,
			SendCh:   sendCh,
		}

		assert.Equal(t, "node-123", nodeConn.NodeUUID)
		assert.Same(t, conn, nodeConn.Conn)
		// Channels are reference types - the value is the same
		assert.NotNil(t, nodeConn.SendCh)
	})
}

// TestErrorMessage tests ErrorMessage structure.
func TestErrorMessage(t *testing.T) {
	t.Run("creates valid error message", func(t *testing.T) {
		msg := node.ErrorMessage{
			Code:    "ERR_INVALID",
			Message: "Invalid message format",
		}

		assert.Equal(t, "ERR_INVALID", msg.Code)
		assert.Equal(t, "Invalid message format", msg.Message)
	})

	t.Run("marshals error message to JSON", func(t *testing.T) {
		msg := node.ErrorMessage{
			Code:    "ERR_TIMEOUT",
			Message: "Operation timed out",
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var unmarshaled node.ErrorMessage
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)

		assert.Equal(t, "ERR_TIMEOUT", unmarshaled.Code)
		assert.Equal(t, "Operation timed out", unmarshaled.Message)
	})
}

// TestToolIdentifier tests parsing tool names into components.
func TestToolIdentifier(t *testing.T) {
	t.Run("parses valid mac tool name", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("mac_a1b2c3_bash")
		assert.Equal(t, "mac", platform)
		assert.Equal(t, "a1b2c3", nodeID)
		assert.Equal(t, "bash", baseTool)
		assert.False(t, isOnHost)
	})

	t.Run("parses valid linux tool name with onhost", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("linux_node123_python_onhost")
		assert.Equal(t, "linux", platform)
		assert.Equal(t, "node123", nodeID)
		assert.Equal(t, "python", baseTool)
		assert.True(t, isOnHost)
	})

	t.Run("parses valid windows tool name", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("windows_win123_powershell")
		assert.Equal(t, "windows", platform)
		assert.Equal(t, "win123", nodeID)
		assert.Equal(t, "powershell", baseTool)
		assert.False(t, isOnHost)
	})

	t.Run("parses valid ios tool name", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("ios_ios123_swift")
		assert.Equal(t, "ios", platform)
		assert.Equal(t, "ios123", nodeID)
		assert.Equal(t, "swift", baseTool)
		assert.False(t, isOnHost)
	})

	t.Run("parses valid android tool name", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("android_droid123_kotlin")
		assert.Equal(t, "android", platform)
		assert.Equal(t, "droid123", nodeID)
		assert.Equal(t, "kotlin", baseTool)
		assert.False(t, isOnHost)
	})

	t.Run("parses valid managed tool name", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("managed_managed123_script")
		assert.Equal(t, "managed", platform)
		assert.Equal(t, "managed123", nodeID)
		assert.Equal(t, "script", baseTool)
		assert.False(t, isOnHost)
	})

	t.Run("returns empty platform for non-node tool", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("bash")
		assert.Empty(t, platform)
		assert.Empty(t, nodeID)
		assert.Equal(t, "bash", baseTool)
		assert.False(t, isOnHost)
	})

	t.Run("returns empty platform for unknown prefix", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("unknown_tool")
		assert.Empty(t, platform)
		assert.Empty(t, nodeID)
		assert.Equal(t, "unknown_tool", baseTool)
		assert.False(t, isOnHost)
	})

	t.Run("handles tool name with underscores in base tool", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("mac_node123_custom_tool_onhost")
		assert.Equal(t, "mac", platform)
		assert.Equal(t, "node123", nodeID)
		assert.Equal(t, "custom", baseTool)
		assert.False(t, isOnHost) // "onhost" is at index 4, not index 3
	})

	t.Run("handles tool name with only 2 parts", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("mac_node123")
		assert.Empty(t, platform)
		assert.Empty(t, nodeID)
		assert.Equal(t, "mac_node123", baseTool)
		assert.False(t, isOnHost)
	})

	t.Run("handles empty string", func(t *testing.T) {
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier("")
		assert.Empty(t, platform)
		assert.Empty(t, nodeID)
		assert.Empty(t, baseTool)
		assert.False(t, isOnHost)
	})
}

// TestIsNodeTool tests identifying node tools.
func TestIsNodeTool(t *testing.T) {
	tests := []struct {
		name       string
		toolName   string
		isNodeTool bool
	}{
		{"mac tool", "mac_node123_bash", true},
		{"linux tool", "linux_node123_python", true},
		{"windows tool", "windows_node123_powershell", true},
		{"ios tool", "ios_node123_swift", true},
		{"android tool", "android_node123_kotlin", true},
		{"managed tool", "managed_node123_script", true},
		{"onhost variant", "mac_node123_bash_onhost", true},
		{"simple tool", "bash", false},
		{"unknown prefix", "unknown_node123_tool", false},
		{"empty string", "", false},
		{"no underscore", "mac", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.isNodeTool, node.IsNodeTool(tt.toolName))
		})
	}
}

// TestGenerateToolName tests generating tool names from components.
func TestGenerateToolName(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		nodeID   string
		toolName string
		isOnHost bool
		expected string
	}{
		{
			name:     "mac tool without host",
			platform: "mac",
			nodeID:   "a1b2c3",
			toolName: "bash",
			isOnHost: false,
			expected: "mac_a1b2c3_bash",
		},
		{
			name:     "linux tool with host",
			platform: "linux",
			nodeID:   "node123",
			toolName: "python",
			isOnHost: true,
			expected: "linux_node123_python_onhost",
		},
		{
			name:     "windows tool",
			platform: "windows",
			nodeID:   "win123",
			toolName: "powershell",
			isOnHost: false,
			expected: "windows_win123_powershell",
		},
		{
			name:     "managed tool with host",
			platform: "managed",
			nodeID:   "m1",
			toolName: "script",
			isOnHost: true,
			expected: "managed_m1_script_onhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := node.GenerateToolName(tt.platform, tt.nodeID, tt.toolName, tt.isOnHost)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGenerateToolName_RoundTrip tests round-trip tool name generation.
func TestGenerateToolName_RoundTrip(t *testing.T) {
	t.Run("round-trips tool name correctly", func(t *testing.T) {
		originalPlatform := "mac"
		originalNodeID := "node123"
		originalTool := "bash"
		originalOnHost := true

		generated := node.GenerateToolName(originalPlatform, originalNodeID, originalTool, originalOnHost)
		platform, nodeID, tool, onHost := node.ToolIdentifier(generated)

		assert.Equal(t, originalPlatform, platform)
		assert.Equal(t, originalNodeID, nodeID)
		assert.Equal(t, originalTool, tool)
		assert.Equal(t, originalOnHost, onHost)
	})
}

// TestNewToolAdapter tests ToolAdapter initialization.
func TestNewToolAdapter(t *testing.T) {
	t.Run("creates adapter with executor", func(t *testing.T) {
		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		assert.NotNil(t, adapter)
	})
}

// TestToolAdapter_AdaptTool tests creating agent.Tool from node tool.
func TestToolAdapter_AdaptTool(t *testing.T) {
	t.Run("creates agent tool with correct properties", func(t *testing.T) {
		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		toolName := "mac_node123_bash"
		tool := adapter.AdaptTool(toolName)

		assert.Equal(t, toolName, tool.Name)
		assert.Contains(t, tool.Description, toolName)
		assert.NotNil(t, tool.Execute)
	})

	t.Run("execute function requires context", func(t *testing.T) {
		mockRegistry := &mockNodeRegistry{}
		executor := node.NewExecutor(node.ExecutorOptions{
			Registry: mockRegistry,
		})
		adapter := node.NewToolAdapter(executor)

		toolName := "linux_node123_python"
		tool := adapter.AdaptTool(toolName)

		ctx := context.Background()
		// Execute will fail because we don't have a real node connection
		assert.NotNil(t, tool.Execute)
		_, err := tool.Execute(ctx, map[string]any{"script": "print('hello')"})
		assert.Error(t, err)
	})
}

// TestToolAdapter_AdaptTool_Execute tests execution behavior.
func TestToolAdapter_AdaptTool_Execute(t *testing.T) {
	t.Run("returns error for invalid tool name", func(t *testing.T) {
		mockRegistry := &mockNodeRegistry{}
		executor := node.NewExecutor(node.ExecutorOptions{
			Registry: mockRegistry,
		})
		adapter := node.NewToolAdapter(executor)

		// Create a tool with an invalid name (not a node tool)
		tool := adapter.AdaptTool("bash")

		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]any{})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid node tool name")
	})

	t.Run("returns error when node is offline", func(t *testing.T) {
		mockRegistry := &mockNodeRegistry{}
		executor := node.NewExecutor(node.ExecutorOptions{
			Registry: mockRegistry,
		})
		adapter := node.NewToolAdapter(executor)

		tool := adapter.AdaptTool("mac_node123_bash")

		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]any{"command": "ls"})

		assert.Error(t, err)
		assert.ErrorIs(t, err, node.ErrNodeOffline)
	})
}

// TestToolAdapter_CreateNodeTools tests bulk tool creation.
func TestToolAdapter_CreateNodeTools(t *testing.T) {
	t.Run("creates tools for all node tools", func(t *testing.T) {
		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		nodeTools := []string{
			"mac_node1_bash",
			"linux_node2_python",
			"windows_node3_powershell",
		}

		tools := adapter.CreateNodeTools(nodeTools)

		assert.Len(t, tools, 3)
		assert.Equal(t, "mac_node1_bash", tools[0].Name)
		assert.Equal(t, "linux_node2_python", tools[1].Name)
		assert.Equal(t, "windows_node3_powershell", tools[2].Name)
	})

	t.Run("filters out non-node tools", func(t *testing.T) {
		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		mixedTools := []string{
			"mac_node1_bash",
			"bash", // Not a node tool
			"linux_node2_python",
			"python", // Not a node tool
		}

		tools := adapter.CreateNodeTools(mixedTools)

		assert.Len(t, tools, 2)
		assert.Equal(t, "mac_node1_bash", tools[0].Name)
		assert.Equal(t, "linux_node2_python", tools[1].Name)
	})

	t.Run("handles empty list", func(t *testing.T) {
		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		tools := adapter.CreateNodeTools([]string{})

		assert.Empty(t, tools)
	})
}

// TestToolAdapter_GetUserNodeTools tests retrieving user's node tools.
func TestToolAdapter_GetUserNodeTools(t *testing.T) {
	t.Run("returns tools for user with one node", func(t *testing.T) {
		mockRegistry := &mockNodeRegistry{
			nodes: map[string][]node.NodeInfo{
				"user1": {
					{
						NodeID:   "node1",
						Platform: "mac",
						Tools:    []string{"bash", "python"},
					},
				},
			},
		}

		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		ctx := context.Background()
		tools := adapter.GetUserNodeTools(ctx, mockRegistry, "user1")

		// Should have bash and python, each with docker and onhost variants
		assert.Len(t, tools, 4) // 2 tools * 2 variants

		// Check for expected tool names
		toolMap := make(map[string]bool)
		for _, t := range tools {
			toolMap[t] = true
		}

		assert.True(t, toolMap["mac_node1_bash"])
		assert.True(t, toolMap["mac_node1_bash_onhost"])
		assert.True(t, toolMap["mac_node1_python"])
		assert.True(t, toolMap["mac_node1_python_onhost"])
	})

	t.Run("returns tools for user with multiple nodes", func(t *testing.T) {
		mockRegistry := &mockNodeRegistry{
			nodes: map[string][]node.NodeInfo{
				"user1": {
					{
						NodeID:   "mac1",
						Platform: "mac",
						Tools:    []string{"bash"},
					},
					{
						NodeID:   "linux1",
						Platform: "linux",
						Tools:    []string{"python"},
					},
				},
			},
		}

		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		ctx := context.Background()
		tools := adapter.GetUserNodeTools(ctx, mockRegistry, "user1")

		assert.Len(t, tools, 4)

		toolMap := make(map[string]bool)
		for _, t := range tools {
			toolMap[t] = true
		}

		assert.True(t, toolMap["mac_mac1_bash"])
		assert.True(t, toolMap["mac_mac1_bash_onhost"])
		assert.True(t, toolMap["linux_linux1_python"])
		assert.True(t, toolMap["linux_linux1_python_onhost"])
	})

	t.Run("managed nodes do not get onhost variant", func(t *testing.T) {
		mockRegistry := &mockNodeRegistry{
			nodes: map[string][]node.NodeInfo{
				"user1": {
					{
						NodeID:   "managed1",
						Platform: "managed",
						Tools:    []string{"script"},
					},
				},
			},
		}

		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		ctx := context.Background()
		tools := adapter.GetUserNodeTools(ctx, mockRegistry, "user1")

		// Only docker variant for managed nodes
		assert.Len(t, tools, 1)
		assert.Equal(t, "managed_managed1_script", tools[0])
	})

	t.Run("returns empty list for user with no nodes", func(t *testing.T) {
		mockRegistry := &mockNodeRegistry{
			nodes: map[string][]node.NodeInfo{},
		}

		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		ctx := context.Background()
		tools := adapter.GetUserNodeTools(ctx, mockRegistry, "user1")

		assert.Empty(t, tools)
	})

	t.Run("returns empty list for non-existent user", func(t *testing.T) {
		mockRegistry := &mockNodeRegistry{
			nodes: map[string][]node.NodeInfo{
				"user1": {},
			},
		}

		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		ctx := context.Background()
		tools := adapter.GetUserNodeTools(ctx, mockRegistry, "user2")

		assert.Empty(t, tools)
	})
}

// TestToolAdapter_ExecuteFunc_OnHost tests execution with onhost flag.
func TestToolAdapter_ExecuteFunc_OnHost(t *testing.T) {
	t.Run("adds onhost suffix for tools with onhost flag", func(t *testing.T) {
		// This tests the internal behavior of createExecuteFunc
		// by checking that the tool identifier properly parses onhost tools
		toolName := "mac_node123_bash_onhost"
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier(toolName)

		assert.Equal(t, "mac", platform)
		assert.Equal(t, "node123", nodeID)
		assert.Equal(t, "bash", baseTool)
		assert.True(t, isOnHost)
	})
}

// TestToolAdapter_EdgeCases tests edge cases and error conditions.
func TestToolAdapter_EdgeCases(t *testing.T) {
	t.Run("handles tool name with multiple underscores", func(t *testing.T) {
		// Tool name with multiple underscores in base tool name
		toolName := "mac_node123_custom_tool_name"
		platform, nodeID, baseTool, isOnHost := node.ToolIdentifier(toolName)

		assert.Equal(t, "mac", platform)
		assert.Equal(t, "node123", nodeID)
		assert.Equal(t, "custom", baseTool) // Only first part after nodeID
		assert.False(t, isOnHost)
	})

	t.Run("handles all platform types", func(t *testing.T) {
		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		platforms := []string{"mac", "linux", "windows", "ios", "android", "managed"}

		for _, platform := range platforms {
			toolName := platform + "_node123_tool"
			tool := adapter.AdaptTool(toolName)
			assert.Equal(t, toolName, tool.Name)

			// Verify it's recognized as a node tool
			assert.True(t, node.IsNodeTool(toolName))
		}
	})
}

// TestNodeToolNameParsing tests comprehensive tool name parsing scenarios.
func TestNodeToolNameParsing(t *testing.T) {
	tests := []struct {
		name           string
		toolName       string
		expectPlatform string
		expectNodeID   string
		expectBaseTool string
		expectOnHost   bool
		expectValid    bool
	}{
		{
			name:           "valid mac tool",
			toolName:       "mac_a1b2c3d4e5f6_bash",
			expectPlatform: "mac",
			expectNodeID:   "a1b2c3d4e5f6",
			expectBaseTool: "bash",
			expectOnHost:   false,
			expectValid:    true,
		},
		{
			name:           "valid linux tool with onhost",
			toolName:       "linux_node-123_python_onhost",
			expectPlatform: "linux",
			expectNodeID:   "node-123",
			expectBaseTool: "python",
			expectOnHost:   true,
			expectValid:    true,
		},
		{
			name:           "valid windows tool",
			toolName:       "windows_win-01_powershell",
			expectPlatform: "windows",
			expectNodeID:   "win-01",
			expectBaseTool: "powershell",
			expectOnHost:   false,
			expectValid:    true,
		},
		{
			name:           "valid ios tool",
			toolName:       "ios_iphone123_swift",
			expectPlatform: "ios",
			expectNodeID:   "iphone123",
			expectBaseTool: "swift",
			expectOnHost:   false,
			expectValid:    true,
		},
		{
			name:           "valid android tool",
			toolName:       "android_pixel7_kotlin",
			expectPlatform: "android",
			expectNodeID:   "pixel7",
			expectBaseTool: "kotlin",
			expectOnHost:   false,
			expectValid:    true,
		},
		{
			name:           "valid managed tool",
			toolName:       "managed_server-01_script",
			expectPlatform: "managed",
			expectNodeID:   "server-01",
			expectBaseTool: "script",
			expectOnHost:   false,
			expectValid:    true,
		},
		{
			name:           "managed with onhost",
			toolName:       "managed_server-01_script_onhost",
			expectPlatform: "managed",
			expectNodeID:   "server-01",
			expectBaseTool: "script",
			expectOnHost:   true,
			expectValid:    true,
		},
		{
			name:           "non-node tool - simple name",
			toolName:       "bash",
			expectPlatform: "",
			expectNodeID:   "",
			expectBaseTool: "bash",
			expectOnHost:   false,
			expectValid:    false,
		},
		{
			name:           "non-node tool - unknown platform",
			toolName:       "unknown_platform_tool",
			expectPlatform: "",
			expectNodeID:   "",
			expectBaseTool: "unknown_platform_tool",
			expectOnHost:   false,
			expectValid:    false,
		},
		{
			name:           "insufficient parts - only platform",
			toolName:       "mac",
			expectPlatform: "",
			expectNodeID:   "",
			expectBaseTool: "mac",
			expectOnHost:   false,
			expectValid:    false,
		},
		{
			name:           "insufficient parts - platform and node only",
			toolName:       "mac_node123",
			expectPlatform: "",
			expectNodeID:   "",
			expectBaseTool: "mac_node123",
			expectOnHost:   false,
			expectValid:    false,
		},
		{
			name:           "empty string",
			toolName:       "",
			expectPlatform: "",
			expectNodeID:   "",
			expectBaseTool: "",
			expectOnHost:   false,
			expectValid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			platform, nodeID, baseTool, isOnHost := node.ToolIdentifier(tt.toolName)

			assert.Equal(t, tt.expectPlatform, platform)
			assert.Equal(t, tt.expectNodeID, nodeID)
			assert.Equal(t, tt.expectBaseTool, baseTool)
			assert.Equal(t, tt.expectOnHost, isOnHost)

			// IsNodeTool should match our expectation
			isValid := node.IsNodeTool(tt.toolName)
			assert.Equal(t, tt.expectValid, isValid)
		})
	}
}

// TestContentBlock tests ContentBlock structure.
func TestContentBlock(t *testing.T) {
	t.Run("creates valid content block", func(t *testing.T) {
		block := node.ContentBlock{
			Type: "text",
			Text: "output",
		}

		assert.Equal(t, "text", block.Type)
		assert.Equal(t, "output", block.Text)
	})
}

// mockNodeRegistry is a mock implementation of NodeRegistry for testing.
type mockNodeRegistry struct {
	nodes map[string][]node.NodeInfo
}

func (m *mockNodeRegistry) GetUserNodes(userID string) []node.NodeInfo {
	if m.nodes == nil {
		return []node.NodeInfo{}
	}
	if nodes, ok := m.nodes[userID]; ok {
		return nodes
	}
	return []node.NodeInfo{}
}

func (m *mockNodeRegistry) GetConnection(nodeID string) (*node.NodeConnection, bool) {
	return nil, false
}

// TestExecuteMessage_IsolationField tests the isolation field.
func TestExecuteMessage_IsolationField(t *testing.T) {
	t.Run("serializes with docker isolation", func(t *testing.T) {
		msg := node.ExecuteMessage{
			ToolName:  "bash",
			Arguments: map[string]interface{}{"cmd": "ls"},
			CallID:    "call-1",
			Isolation: "docker",
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, "docker", parsed["isolation"])
	})

	t.Run("serializes with host isolation", func(t *testing.T) {
		msg := node.ExecuteMessage{
			ToolName:  "bash",
			Arguments: map[string]interface{}{"cmd": "ls"},
			CallID:    "call-2",
			Isolation: "host",
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		assert.Equal(t, "host", parsed["isolation"])
	})

	t.Run("omits isolation field when empty", func(t *testing.T) {
		msg := node.ExecuteMessage{
			ToolName:  "bash",
			Arguments: map[string]interface{}{"cmd": "ls"},
			CallID:    "call-3",
		}

		data, err := json.Marshal(msg)
		require.NoError(t, err)

		var parsed map[string]interface{}
		err = json.Unmarshal(data, &parsed)
		require.NoError(t, err)

		_, hasIsolation := parsed["isolation"]
		assert.False(t, hasIsolation)
	})
}

// TestToolAdapter_AdaptToolDescription tests tool description generation.
func TestToolAdapter_AdaptToolDescription(t *testing.T) {
	t.Run("includes tool name in description", func(t *testing.T) {
		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		toolName := "mac_node123_bash"
		tool := adapter.AdaptTool(toolName)

		assert.Contains(t, tool.Description, toolName)
	})

	t.Run("description format is consistent", func(t *testing.T) {
		executor := &node.Executor{}
		adapter := node.NewToolAdapter(executor)

		toolName := "linux_node456_python"
		tool := adapter.AdaptTool(toolName)

		expectedPrefix := "Execute "
		assert.True(t, len(tool.Description) > len(expectedPrefix))
	})
}
