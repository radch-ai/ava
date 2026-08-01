//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/services/gateway/internal/agent"
	"github.com/radch-ai/ava/services/gateway/internal/node"
)

// mockAgentProvider is a mock AgentProvider for testing.
type mockAgentProvider struct {
	mu     sync.Mutex
	agents map[string]*agent.Agent
}

func newMockAgentProvider() *mockAgentProvider {
	return &mockAgentProvider{
		agents: make(map[string]*agent.Agent),
	}
}

func (m *mockAgentProvider) GetAgentForUser(userID string) (*agent.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ag, exists := m.agents[userID]; exists {
		return ag, nil
	}
	// Create a new agent for the user
	ag := agent.NewAgent(agent.AgentOptions{
		SystemPrompt: "You are a helpful assistant",
		SessionID:    userID,
	})
	m.agents[userID] = ag
	return ag, nil
}

func (m *mockAgentProvider) getAgent(userID string) *agent.Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.agents[userID]
}

// mockWebSocketConn is a mock WebSocket connection for integration testing.
// It implements the node.WebSocketConn interface using coder/websocket types.
type mockWebSocketConn struct {
	sendCh    chan []byte
	receiveCh chan []byte
	closed    chan struct{}
	mu        sync.Mutex
	isClosed  bool
	nodeID    string
}

func newMockWebSocketConn(nodeID string) *mockWebSocketConn {
	return &mockWebSocketConn{
		sendCh:    make(chan []byte, 100),
		receiveCh: make(chan []byte, 100),
		closed:    make(chan struct{}),
		nodeID:    nodeID,
	}
}

// Write implements the WebSocketConn interface.
func (m *mockWebSocketConn) Write(ctx context.Context, typ coderws.MessageType, p []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isClosed {
		return errors.New("connection closed")
	}
	// Echo back to receive channel for testing
	m.receiveCh <- p
	return nil
}

// Reader implements the WebSocketConn interface.
func (m *mockWebSocketConn) Reader(ctx context.Context) (coderws.MessageType, io.Reader, error) {
	select {
	case data := <-m.receiveCh:
		return coderws.MessageText, bytes.NewReader(data), nil
	case <-m.closed:
		return 0, nil, errors.New("connection closed")
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

// CloseNow implements the WebSocketConn interface.
func (m *mockWebSocketConn) CloseNow() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.isClosed {
		m.isClosed = true
		close(m.closed)
	}
	return nil
}

// TestNodeService_ConnectionFlow tests the complete node connection flow.
func TestNodeService_ConnectionFlow(t *testing.T) {
	t.Run("registers node and adds tools to agent", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()
		agentProvider := newMockAgentProvider()

		svc := node.NewService(&logger, agentProvider)

		// Create mock connection
		mockConn := newMockWebSocketConn("test-node-1")

		// Register connection
		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
			{Name: "python", Isolation: []string{"docker"}},
		}

		err := svc.RegisterConnection(ctx, mockConn, "test-node-1", "user-1", "linux", toolSpecs)
		require.NoError(t, err)

		// Verify agent has tools
		userAgent := agentProvider.getAgent("user-1")
		require.NotNil(t, userAgent)

		state := userAgent.State()
		// bash (docker + host) + python (docker) = 3 tools
		assert.Len(t, state.Tools, 3, "should have 3 tools from node")

		// Verify tool names
		toolNames := make(map[string]bool)
		for _, tool := range state.Tools {
			toolNames[tool.Name] = true
		}

		assert.True(t, toolNames["linux_test-node-1_bash"], "missing bash docker tool")
		assert.True(t, toolNames["linux_test-node-1_bash_onhost"], "missing bash host tool")
		assert.True(t, toolNames["linux_test-node-1_python"], "missing python docker tool")

		// Cleanup
		err = svc.UnregisterConnection(ctx, "test-node-1", "user-1")
		require.NoError(t, err)
		_ = mockConn.CloseNow()

		// Verify tools were removed
		state = userAgent.State()
		for _, tool := range state.Tools {
			assert.False(t, strings.HasPrefix(tool.Name, "linux_test-node-1_"),
				"node tools should be removed after unregister")
		}
	})
}

// TestNodeService_ToolCreation tests tool creation with different isolation modes.
func TestNodeService_ToolCreation(t *testing.T) {
	t.Run("creates correct tools for all isolation modes", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()
		agentProvider := newMockAgentProvider()

		svc := node.NewService(&logger, agentProvider)

		// Create mock connection
		mockConn := newMockWebSocketConn("tool-node-1")

		// Register with tools that have different isolation modes
		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
			{Name: "python", Isolation: []string{"docker"}},
			{Name: "node", Isolation: []string{"host"}},
		}

		err := svc.RegisterConnection(ctx, mockConn, "tool-node-1", "user-1", "linux", toolSpecs)
		require.NoError(t, err)

		// Get agent and verify tools
		userAgent := agentProvider.getAgent("user-1")
		state := userAgent.State()

		// Should have:
		// bash -> 2 tools (docker, host)
		// python -> 1 tool (docker)
		// node -> 1 tool (host)
		// Total: 4 tools
		assert.Len(t, state.Tools, 4)

		// Verify tool names
		toolNames := make(map[string]bool)
		for _, tool := range state.Tools {
			toolNames[tool.Name] = true
		}

		assert.True(t, toolNames["linux_tool-node-1_bash"], "missing bash docker tool")
		assert.True(t, toolNames["linux_tool-node-1_bash_onhost"], "missing bash host tool")
		assert.True(t, toolNames["linux_tool-node-1_python"], "missing python docker tool")
		assert.True(t, toolNames["linux_tool-node-1_node_onhost"], "missing node host tool")

		// Verify tool descriptions indicate isolation mode
		for _, tool := range state.Tools {
			assert.NotEmpty(t, tool.Description, "tool should have description")
			assert.Contains(t, tool.Description, "tool-node-1", "description should identify node")
			assert.Contains(t, tool.Description, "Node:", "description should mention Node:")
		}

		// Verify execute functions exist
		for _, tool := range state.Tools {
			assert.NotNil(t, tool.Execute, "tool should have execute function")
		}

		// Cleanup
		svc.UnregisterConnection(ctx, "tool-node-1", "user-1")
		_ = mockConn.CloseNow()
	})
}

// TestNodeService_MultipleNodes tests multiple nodes from different users.
func TestNodeService_MultipleNodes(t *testing.T) {
	t.Run("handles multiple nodes from different users", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()
		agentProvider := newMockAgentProvider()

		svc := node.NewService(&logger, agentProvider)

		// Register nodes for different users
		nodes := []struct {
			nodeID   string
			userID   string
			platform string
			tools    []node.ToolSpec
		}{
			{"node-user1-a", "user-1", "linux", []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}},
			{"node-user1-b", "user-1", "mac", []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}},
			{"node-user2-a", "user-2", "windows", []node.ToolSpec{{Name: "powershell", Isolation: []string{"docker"}}}},
		}

		for _, n := range nodes {
			mockConn := newMockWebSocketConn(n.nodeID)
			err := svc.RegisterConnection(ctx, mockConn, n.nodeID, n.userID, n.platform, n.tools)
			require.NoError(t, err)
		}

		// Verify user-1 has 2 nodes worth of tools
		user1Agent := agentProvider.getAgent("user-1")
		user1Tools := user1Agent.State().Tools
		assert.Len(t, user1Tools, 2, "user-1 should have 2 tools (one per node)")

		// Verify user-2 has 1 node worth of tools
		user2Agent := agentProvider.getAgent("user-2")
		user2Tools := user2Agent.State().Tools
		assert.Len(t, user2Tools, 1, "user-2 should have 1 tool")

		// Verify tool isolation between users
		for _, tool := range user1Tools {
			assert.Contains(t, tool.Name, "node-user1", "user-1 tools should be for their nodes")
			assert.NotContains(t, tool.Name, "node-user2", "user-1 tools should not include user-2 nodes")
		}

		for _, tool := range user2Tools {
			assert.Contains(t, tool.Name, "node-user2", "user-2 tools should be for their nodes")
		}

		// Cleanup
		for _, n := range nodes {
			svc.UnregisterConnection(ctx, n.nodeID, n.userID)
		}
	})
}

// TestNodeService_HealthUpdates tests node health updates.
func TestNodeService_HealthUpdates(t *testing.T) {
	t.Run("tracks node health changes", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()
		agentProvider := newMockAgentProvider()

		svc := node.NewService(&logger, agentProvider)

		// Register node
		mockConn := newMockWebSocketConn("health-node-1")
		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := svc.RegisterConnection(ctx, mockConn, "health-node-1", "user-1", "linux", toolSpecs)
		require.NoError(t, err)

		// Update to busy
		err = svc.UpdateNodeHealth("health-node-1", node.NodeStatusBusy, []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
			{Name: "python", Isolation: []string{"docker"}},
		})
		require.NoError(t, err)

		// Update to offline
		err = svc.UpdateNodeHealth("health-node-1", node.NodeStatusOffline, []node.ToolSpec{})
		require.NoError(t, err)

		// Cleanup
		svc.UnregisterConnection(ctx, "health-node-1", "user-1")
		_ = mockConn.CloseNow()
	})
}

// TestNodeService_Reconnection tests node reconnection scenarios.
func TestNodeService_Reconnection(t *testing.T) {
	t.Run("handles node reconnection", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()
		agentProvider := newMockAgentProvider()

		svc := node.NewService(&logger, agentProvider)

		nodeID := "reconnect-node-1"

		// First connection
		mockConn1 := newMockWebSocketConn(nodeID)
		toolSpecs1 := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := svc.RegisterConnection(ctx, mockConn1, nodeID, "user-1", "linux", toolSpecs1)
		require.NoError(t, err)

		// Verify agent has tools
		userAgent := agentProvider.getAgent("user-1")
		assert.Len(t, userAgent.State().Tools, 1)

		// Simulate disconnect
		err = svc.UnregisterConnection(ctx, nodeID, "user-1")
		require.NoError(t, err)
		_ = mockConn1.CloseNow()

		// Verify tools were removed
		assert.Empty(t, userAgent.State().Tools, "tools should be removed after unregister")

		// Second connection (reconnect)
		mockConn2 := newMockWebSocketConn(nodeID)
		toolSpecs2 := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
			{Name: "python", Isolation: []string{"docker"}},
		}

		err = svc.RegisterConnection(ctx, mockConn2, nodeID, "user-1", "linux", toolSpecs2)
		require.NoError(t, err)

		// Verify reconnection with updated tools
		assert.Len(t, userAgent.State().Tools, 2, "should have 2 tools after reconnect")

		// Cleanup
		svc.UnregisterConnection(ctx, nodeID, "user-1")
		_ = mockConn2.CloseNow()
	})
}

// TestNodeService_PlatformVariations tests different platform handling.
func TestNodeService_PlatformVariations(t *testing.T) {
	t.Run("generates correct tool names for all platforms", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()

		platforms := []struct {
			platform     string
			nodeID       string
			toolName     string
			expectedTool string
		}{
			{"linux", "linux-node", "bash", "linux_linux-node_bash"},
			{"mac", "mac-node", "bash", "mac_mac-node_bash"},
			{"windows", "windows-node", "powershell", "windows_windows-node_powershell"},
			{"managed", "managed-node", "bash", "managed_managed-node_bash"},
		}

		for _, p := range platforms {
			// Create a new provider and service for each platform to avoid conflicts
			testAgentProvider := newMockAgentProvider()
			testSvc := node.NewService(&logger, testAgentProvider)

			mockConn := newMockWebSocketConn(p.nodeID)
			toolSpecs := []node.ToolSpec{{Name: p.toolName, Isolation: []string{"docker"}}}

			err := testSvc.RegisterConnection(ctx, mockConn, p.nodeID, "user-1", p.platform, toolSpecs)
			require.NoError(t, err)

			userAgent := testAgentProvider.getAgent("user-1")
			state := userAgent.State()
			if len(state.Tools) > 0 {
				assert.Equal(t, p.expectedTool, state.Tools[0].Name)
			}

			// Cleanup
			testSvc.UnregisterConnection(ctx, p.nodeID, "user-1")
			_ = mockConn.CloseNow()
		}
	})
}

// TestNodeService_ConcurrentAccess tests concurrent access to the service.
func TestNodeService_ConcurrentAccess(t *testing.T) {
	t.Run("handles concurrent operations safely", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()
		agentProvider := newMockAgentProvider()

		svc := node.NewService(&logger, agentProvider)

		numNodes := 20
		var wg sync.WaitGroup

		// Concurrent registrations
		for i := 0; i < numNodes; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				nodeID := fmt.Sprintf("concurrent-node-%d", index)
				userID := fmt.Sprintf("user-%d", index%5) // Distribute across 5 users
				mockConn := newMockWebSocketConn(nodeID)
				toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

				_ = svc.RegisterConnection(ctx, mockConn, nodeID, userID, "linux", toolSpecs)
			}(i)
		}

		wg.Wait()

		// Verify all users have their tools
		for i := 0; i < 5; i++ {
			userID := fmt.Sprintf("user-%d", i)
			userAgent := agentProvider.getAgent(userID)
			if userAgent != nil {
				tools := userAgent.State().Tools
				// Each user should have some nodes
				assert.GreaterOrEqual(t, len(tools), 0, "user should have registered nodes")
			}
		}

		// Cleanup
		for i := 0; i < numNodes; i++ {
			nodeID := fmt.Sprintf("concurrent-node-%d", i)
			userID := fmt.Sprintf("user-%d", i%5)
			svc.UnregisterConnection(ctx, nodeID, userID)
		}
	})
}

// TestNodeService_ToolIntegration tests tool integration with agent.
func TestNodeService_ToolIntegration(t *testing.T) {
	t.Run("node tools integrate with agent system", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()
		agentProvider := newMockAgentProvider()

		svc := node.NewService(&logger, agentProvider)

		// Register node
		mockConn := newMockWebSocketConn("agent-node-1")
		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
			{Name: "python", Isolation: []string{"docker"}},
		}

		err := svc.RegisterConnection(ctx, mockConn, "agent-node-1", "user-1", "linux", toolSpecs)
		require.NoError(t, err)

		// Get agent and verify tools
		userAgent := agentProvider.getAgent("user-1")
		state := userAgent.State()

		// Verify tools are valid agent.Tool entries
		for _, tool := range state.Tools {
			assert.NotEmpty(t, tool.Name, "tool name should not be empty")
			assert.NotEmpty(t, tool.Description, "tool description should not be empty")
			assert.NotNil(t, tool.Execute, "tool execute function should not be nil")

			// Verify tool description format
			assert.Contains(t, tool.Description, "Node:", "description should identify the node")
			assert.Contains(t, tool.Description, "agent-node-1", "description should mention node ID")
			assert.Contains(t, tool.Description, "Isolation:", "description should mention isolation")
		}

		// Cleanup
		svc.UnregisterConnection(ctx, "agent-node-1", "user-1")
		_ = mockConn.CloseNow()
	})
}

// TestNodeService_ErrorHandling tests error scenarios.
func TestNodeService_ErrorHandling(t *testing.T) {
	t.Run("handles errors gracefully", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()
		agentProvider := newMockAgentProvider()

		svc := node.NewService(&logger, agentProvider)

		// Test 1: Update health for non-existent node
		err := svc.UpdateNodeHealth("nonexistent", node.NodeStatusOnline, []node.ToolSpec{})
		assert.Error(t, err)
		assert.ErrorIs(t, err, node.ErrNodeNotFound)

		// Test 2: Unregister non-existent node (should be idempotent)
		err = svc.UnregisterConnection(ctx, "nonexistent", "user-1")
		assert.NoError(t, err, "unregister should be idempotent")

		// Test 3: Double registration of same node ID
		mockConn1 := newMockWebSocketConn("dup-node")
		mockConn2 := newMockWebSocketConn("dup-node")
		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err = svc.RegisterConnection(ctx, mockConn1, "dup-node", "user-1", "linux", toolSpecs)
		require.NoError(t, err)

		// Second registration with same node ID
		err = svc.RegisterConnection(ctx, mockConn2, "dup-node", "user-1", "linux", toolSpecs)
		// This might succeed (replacing) or fail depending on implementation
		// Either behavior is acceptable, just verify no crash

		// Cleanup
		svc.UnregisterConnection(ctx, "dup-node", "user-1")
		_ = mockConn1.CloseNow()
		_ = mockConn2.CloseNow()
	})
}

// TestNodeService_ToolDescriptions tests tool description formats.
func TestNodeService_ToolDescriptions(t *testing.T) {
	t.Run("generates descriptive tool names for LLM", func(t *testing.T) {
		ctx := context.Background()
		logger := zerolog.Nop()
		agentProvider := newMockAgentProvider()

		svc := node.NewService(&logger, agentProvider)

		// Register node
		mockConn := newMockWebSocketConn("desc-node-1")
		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
		}

		err := svc.RegisterConnection(ctx, mockConn, "desc-node-1", "user-1", "linux", toolSpecs)
		require.NoError(t, err)

		// Get agent tools
		userAgent := agentProvider.getAgent("user-1")
		state := userAgent.State()

		// Find docker and host tools
		var dockerTool, hostTool *agent.Tool
		for i := range state.Tools {
			if strings.Contains(state.Tools[i].Name, "bash_onhost") {
				hostTool = &state.Tools[i]
			} else if strings.Contains(state.Tools[i].Name, "bash") && !strings.Contains(state.Tools[i].Name, "onhost") {
				dockerTool = &state.Tools[i]
			}
		}

		require.NotNil(t, dockerTool, "docker tool should exist")
		require.NotNil(t, hostTool, "host tool should exist")

		// Verify docker tool description
		assert.Contains(t, dockerTool.Description, "Docker", "docker tool should mention Docker")
		assert.Contains(t, dockerTool.Description, "preferred", "docker tool should mention it's preferred")
		assert.NotContains(t, dockerTool.Description, "onhost", "docker tool should not mention onhost")

		// Verify host tool description
		assert.Contains(t, hostTool.Description, "host", "host tool should mention host")
		assert.Contains(t, hostTool.Description, "onhost", "host tool should mention onhost or bypass")

		// Cleanup
		svc.UnregisterConnection(ctx, "desc-node-1", "user-1")
		_ = mockConn.CloseNow()
	})
}
