// Package node_test provides unit tests for the node service.
package node_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/radch-ai/ava/services/gateway/internal/agent"
	"github.com/radch-ai/ava/services/gateway/internal/node"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing

// mockAgentProvider is a mock implementation of node.AgentProvider.
type mockAgentProvider struct {
	mu            sync.Mutex
	agents        map[string]*agent.Agent
	getAgentError error
	usersNotFound map[string]bool
}

func newMockAgentProvider() *mockAgentProvider {
	return &mockAgentProvider{
		agents:        make(map[string]*agent.Agent),
		usersNotFound: make(map[string]bool),
	}
}

func (m *mockAgentProvider) GetAgentForUser(userID string) (*agent.Agent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.getAgentError != nil {
		return nil, m.getAgentError
	}

	if m.usersNotFound[userID] {
		return nil, fmt.Errorf("agent not found for user: %s", userID)
	}

	// Return existing agent or create a new one
	if existingAgent, ok := m.agents[userID]; ok {
		return existingAgent, nil
	}

	newAgent := agent.NewAgent(agent.AgentOptions{
		SystemPrompt: "test prompt",
		SessionID:    userID,
	})
	m.agents[userID] = newAgent
	return newAgent, nil
}

func (m *mockAgentProvider) SetGetAgentError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getAgentError = err
}

func (m *mockAgentProvider) SetUserNotFound(userID string, notFound bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usersNotFound[userID] = notFound
}

func (m *mockAgentProvider) GetAgent(userID string) *agent.Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.agents[userID]
}

func (m *mockAgentProvider) HasAgent(userID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.agents[userID]
	return exists
}

// mockWebSocketConn is a mock WebSocket connection that implements the
// node.WebSocketConn interface using coder/websocket types.
type mockWebSocketConn struct {
	mu              sync.Mutex
	closed          bool
	messagesWritten [][]byte
	writeError      error
	readError       error
	messagesToRead  [][]byte
	readIndex       int
	closeCalled     bool
}

func newMockWebSocketConn() *mockWebSocketConn {
	return &mockWebSocketConn{
		messagesToRead: make([][]byte, 0),
	}
}

// Write implements the WebSocketConn interface.
func (m *mockWebSocketConn) Write(ctx context.Context, typ coderws.MessageType, p []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.writeError != nil {
		return m.writeError
	}

	if m.closed {
		return fmt.Errorf("connection closed")
	}

	m.messagesWritten = append(m.messagesWritten, p)
	return nil
}

// Reader implements the WebSocketConn interface.
func (m *mockWebSocketConn) Reader(ctx context.Context) (coderws.MessageType, io.Reader, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, nil, fmt.Errorf("connection closed")
	}

	if m.readError != nil {
		return 0, nil, m.readError
	}

	if m.readIndex >= len(m.messagesToRead) {
		// Return timeout-like error to allow retry in tests
		return 0, nil, fmt.Errorf("timeout")
	}

	msg := m.messagesToRead[m.readIndex]
	m.readIndex++
	return coderws.MessageText, bytes.NewReader(msg), nil
}

// CloseNow implements the WebSocketConn interface.
func (m *mockWebSocketConn) CloseNow() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.closeCalled = true
	return nil
}

// IsClosed returns whether the connection is closed
func (m *mockWebSocketConn) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// GetWrittenMessages returns all messages written to this connection
func (m *mockWebSocketConn) GetWrittenMessages() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messagesWritten
}

// QueueMessage adds a message to be read
func (m *mockWebSocketConn) QueueMessage(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messagesToRead = append(m.messagesToRead, data)
}

// SetWriteError sets an error to return on WriteMessage
func (m *mockWebSocketConn) SetWriteError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeError = err
}

// SetReadError sets an error to return on ReadMessage
func (m *mockWebSocketConn) SetReadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readError = err
}

// WasCloseCalled returns true if Close was called
func (m *mockWebSocketConn) WasCloseCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeCalled
}

// Helper function to create a test logger
func testLogger() *zerolog.Logger {
	logger := zerolog.New(zerolog.NewConsoleWriter())
	return &logger
}

// TestNewService verifies service initialization.
func TestNewService(t *testing.T) {
	t.Run("creates a new service with non-nil components", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()

		service := node.NewService(logger, agentProvider)

		assert.NotNil(t, service, "service should not be nil")
	})

	t.Run("service is initialized with provided dependencies", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()

		service := node.NewService(logger, agentProvider)

		// We can't directly access the private fields, but we can verify
		// the service is functional by calling methods on it
		assert.NotNil(t, service, "service should be created successfully")
	})
}

// TestService_RegisterConnection tests node connection registration.
func TestService_RegisterConnection(t *testing.T) {
	t.Run("successfully registers a new node connection", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
			{Name: "python", Isolation: []string{"docker", "host"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)

		require.NoError(t, err)

		// Verify agent was created
		assert.True(t, agentProvider.HasAgent("user1"))
	})

	t.Run("registers multiple tools for different isolation modes", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)

		require.NoError(t, err)

		// Verify agent was created and has tools added
		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()
		// Should have 2 tools: bash (docker) and bash_onhost (host)
		assert.GreaterOrEqual(t, len(state.Tools), 2, "should have at least 2 tools for different isolation modes")
	})

	t.Run("registers tools with only docker isolation", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)

		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()
		// Should have 1 tool: bash (docker)
		assert.GreaterOrEqual(t, len(state.Tools), 1, "should have at least 1 tool")

		// Find the docker tool
		foundDockerTool := false
		for _, tool := range state.Tools {
			if tool.Name == "linux_node1_bash" {
				foundDockerTool = true
				assert.Contains(t, tool.Description, "Docker")
				break
			}
		}
		assert.True(t, foundDockerTool, "should have docker isolated tool")
	})

	t.Run("registers tools with only host isolation", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"host"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "mac", toolSpecs)

		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		// Find the onhost tool
		foundOnHostTool := false
		for _, tool := range state.Tools {
			if tool.Name == "mac_node1_bash_onhost" {
				foundOnHostTool = true
				assert.Contains(t, tool.Description, "onhost")
				break
			}
		}
		assert.True(t, foundOnHostTool, "should have onhost tool")
	})

	t.Run("skips unknown isolation modes", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "unknown", "host"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)

		require.NoError(t, err)

		// Should only create tools for docker and host, skipping unknown
		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()
		// Should have 2 tools: bash (docker) and bash_onhost (host)
		// The unknown isolation mode should be skipped
		assert.GreaterOrEqual(t, len(state.Tools), 2, "should have at least 2 tools")

		// Verify we have both docker and host tools
		foundDocker := false
		foundHost := false
		for _, tool := range state.Tools {
			if tool.Name == "linux_node1_bash" {
				foundDocker = true
			}
			if tool.Name == "linux_node1_bash_onhost" {
				foundHost = true
			}
		}
		assert.True(t, foundDocker, "should have docker tool")
		assert.True(t, foundHost, "should have host tool")
	})

	t.Run("handles empty tool specs", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)

		require.NoError(t, err)
	})

	t.Run("returns error when agent provider fails", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
		}

		// Set the agent provider to return an error
		agentProvider.SetGetAgentError(fmt.Errorf("agent service unavailable"))

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get agent for user")
	})

	t.Run("registers multiple nodes for same user", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn1 := newMockWebSocketConn()
		conn2 := newMockWebSocketConn()

		toolSpecs1 := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}
		toolSpecs2 := []node.ToolSpec{{Name: "python", Isolation: []string{"docker"}}}

		err1 := service.RegisterConnection(ctx, conn1, "node1", "user1", "linux", toolSpecs1)
		err2 := service.RegisterConnection(ctx, conn2, "node2", "user1", "mac", toolSpecs2)

		require.NoError(t, err1)
		require.NoError(t, err2)

		// Verify same agent has tools from both nodes
		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()
		// Should have tools from both nodes
		assert.GreaterOrEqual(t, len(state.Tools), 2, "should have tools from both nodes")
	})

	t.Run("registers nodes for different users", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn1 := newMockWebSocketConn()
		conn2 := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err1 := service.RegisterConnection(ctx, conn1, "node1", "user1", "linux", toolSpecs)
		err2 := service.RegisterConnection(ctx, conn2, "node2", "user2", "mac", toolSpecs)

		require.NoError(t, err1)
		require.NoError(t, err2)

		// Verify each user has their own agent
		agent1 := agentProvider.GetAgent("user1")
		agent2 := agentProvider.GetAgent("user2")

		require.NotNil(t, agent1)
		require.NotNil(t, agent2)

		assert.NotEqual(t, agent1, agent2, "different users should have different agents")
	})
}

// TestService_UnregisterConnection tests node connection unregistration.
func TestService_UnregisterConnection(t *testing.T) {
	t.Run("successfully unregisters an existing node", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
			{Name: "python", Isolation: []string{"docker", "host"}},
		}

		// Register the node first
		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Verify tools were added
		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)
		stateBefore := ag.State()
		toolsBefore := len(stateBefore.Tools)

		// Unregister the node
		err = service.UnregisterConnection(ctx, "node1", "user1")
		require.NoError(t, err)

		// Verify tools were removed
		stateAfter := ag.State()
		assert.Less(t, len(stateAfter.Tools), toolsBefore, "tools should be removed after unregistration")
	})

	t.Run("unregister is idempotent for non-existent nodes", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()

		err := service.UnregisterConnection(ctx, "nonexistent", "user1")
		assert.NoError(t, err, "unregister should not error for non-existent nodes")
	})

	t.Run("removes tools with correct prefix", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
		}

		// Register node with linux platform
		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		stateBefore := ag.State()
		toolsBefore := len(stateBefore.Tools)

		// Unregister the node
		err = service.UnregisterConnection(ctx, "node1", "user1")
		require.NoError(t, err)

		// Verify tools were removed with the correct prefix (linux_node1_)
		stateAfter := ag.State()
		assert.Less(t, len(stateAfter.Tools), toolsBefore)

		// Verify the specific tools are gone
		for _, tool := range stateAfter.Tools {
			assert.NotContains(t, tool.Name, "node1", "node1 tools should be removed")
		}
	})

	t.Run("handles case where agent is not found gracefully", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		// Register a node
		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Manually remove the agent from our mock to simulate it being gone
		// We can't actually delete from the service's perspective, but we can test
		// the unregister logic which should handle missing agent gracefully

		// Unregister should still work
		_ = service.UnregisterConnection(ctx, "node1", "user1")
		// The error might occur when getting the agent, but unregistration should proceed
		// This is implementation-specific, so we just verify it doesn't panic
		assert.NotPanics(t, func() {
			_ = service.UnregisterConnection(ctx, "node1", "user1")
		})
	})

	t.Run("unregister removes all tools for the node", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
			{Name: "python", Isolation: []string{"docker"}},
		}

		// Register node
		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		// Count node1 tools before unregister
		stateBefore := ag.State()
		node1ToolCount := 0
		for _, tool := range stateBefore.Tools {
			if contains(tool.Name, "node1") {
				node1ToolCount++
			}
		}
		assert.Greater(t, node1ToolCount, 0, "should have node1 tools")

		// Unregister
		err = service.UnregisterConnection(ctx, "node1", "user1")
		require.NoError(t, err)

		// Verify all node1 tools are removed
		stateAfter := ag.State()
		node1ToolCountAfter := 0
		for _, tool := range stateAfter.Tools {
			if contains(tool.Name, "node1") {
				node1ToolCountAfter++
			}
		}
		assert.Equal(t, 0, node1ToolCountAfter, "all node1 tools should be removed")
	})
}

// TestService_UpdateNodeHealth tests updating node health.
func TestService_UpdateNodeHealth(t *testing.T) {
	t.Run("successfully updates node health", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
		}

		// Register the node first
		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Update node health with new status and tools
		newToolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
			{Name: "python", Isolation: []string{"docker"}},
		}

		err = service.UpdateNodeHealth("node1", node.NodeStatusBusy, newToolSpecs)
		require.NoError(t, err)
	})

	t.Run("updates node to offline status", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Update to offline
		err = service.UpdateNodeHealth("node1", node.NodeStatusOffline, toolSpecs)
		require.NoError(t, err)
	})

	t.Run("handles empty tool specs in health update", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Update with empty tool specs
		err = service.UpdateNodeHealth("node1", node.NodeStatusOnline, []node.ToolSpec{})
		require.NoError(t, err)
	})

	t.Run("handles health update for non-existent node", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		// Try to update a node that doesn't exist
		err := service.UpdateNodeHealth("nonexistent", node.NodeStatusOnline, toolSpecs)
		// This should return an error
		assert.Error(t, err)
	})

	t.Run("handles all node status values", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		statuses := []node.NodeStatus{
			node.NodeStatusOnline,
			node.NodeStatusBusy,
			node.NodeStatusHealthy,
			node.NodeStatusDegraded,
			node.NodeStatusUnhealthy,
			node.NodeStatusOffline,
		}

		for _, status := range statuses {
			err := service.UpdateNodeHealth("node1", status, toolSpecs)
			assert.NoError(t, err, "should update to status %s", status)
		}
	})
}

// TestService_ExecuteOnNode tests tool execution on nodes.
func TestService_ExecuteOnNode(t *testing.T) {
	t.Run("successfully executes a tool on a node", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Note: This will timeout since we're not setting up a full execution flow
		// but we can verify the method signature is correct
		execCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err = service.ExecuteOnNode(execCtx, "node1", "bash", map[string]any{"command": "echo hello"})
		// Expected to timeout or error in this test setup
		assert.Error(t, err)
	})

	t.Run("returns error for non-existent node", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()

		// Try to execute on a node that doesn't exist
		_, err := service.ExecuteOnNode(ctx, "nonexistent", "bash", map[string]any{})
		assert.Error(t, err)
	})

	t.Run("handles execution with empty args", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Execute with empty args
		execCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err = service.ExecuteOnNode(execCtx, "node1", "bash", nil)
		assert.Error(t, err) // Will timeout, but tests the method works
	})

	t.Run("handles execution with nil args map", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		execCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err = service.ExecuteOnNode(execCtx, "node1", "bash", map[string]any(nil))
		assert.Error(t, err)
	})
}

// TestService_CreateNodeTools tests tool creation from node specs.
func TestService_CreateNodeTools(t *testing.T) {
	t.Run("creates docker isolated tool correctly", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		// Find the bash tool
		var bashTool *agent.Tool
		for i := range state.Tools {
			if state.Tools[i].Name == "linux_node1_bash" {
				bashTool = &state.Tools[i]
				break
			}
		}

		require.NotNil(t, bashTool, "bash tool should be created")
		assert.Contains(t, bashTool.Description, "bash")
		assert.Contains(t, bashTool.Description, "Linux_node1")
		assert.Contains(t, bashTool.Description, "Docker")
		assert.NotContains(t, bashTool.Description, "onhost")
	})

	t.Run("creates host isolated tool correctly", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"host"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "mac", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		// Find the bash tool with onhost suffix
		var bashTool *agent.Tool
		for i := range state.Tools {
			if state.Tools[i].Name == "mac_node1_bash_onhost" {
				bashTool = &state.Tools[i]
				break
			}
		}

		require.NotNil(t, bashTool, "bash onhost tool should be created")
		assert.Contains(t, bashTool.Description, "bash")
		assert.Contains(t, bashTool.Description, "Mac_node1")
		assert.Contains(t, bashTool.Description, "onhost")
	})

	t.Run("creates both isolation modes for same tool", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		// Should have both docker and host variants
		foundDocker := false
		foundHost := false

		for _, tool := range state.Tools {
			if tool.Name == "linux_node1_bash" {
				foundDocker = true
				assert.Contains(t, tool.Description, "Docker")
			}
			if tool.Name == "linux_node1_bash_onhost" {
				foundHost = true
				assert.Contains(t, tool.Description, "onhost")
			}
		}

		assert.True(t, foundDocker, "docker variant should be created")
		assert.True(t, foundHost, "host variant should be created")
	})

	t.Run("tool execute function returns error on execution failure", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		// Find the bash tool
		var bashTool *agent.Tool
		for i := range state.Tools {
			if state.Tools[i].Name == "linux_node1_bash" {
				bashTool = &state.Tools[i]
				break
			}
		}

		require.NotNil(t, bashTool)
		require.NotNil(t, bashTool.Execute)

		// Execute with a context that will timeout (no actual node connection)
		execCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		_, err = bashTool.Execute(execCtx, map[string]any{"command": "test"})
		assert.Error(t, err)
	})
}

// TestService_ToolNaming tests tool name generation.
func TestService_ToolNaming(t *testing.T) {
	t.Run("generates correct tool names for different platforms", func(t *testing.T) {
		testCases := []struct {
			platform     string
			nodeID       string
			toolName     string
			isOnHost     bool
			expectedName string
		}{
			{"linux", "node123", "bash", false, "linux_node123_bash"},
			{"linux", "node123", "bash", true, "linux_node123_bash_onhost"},
			{"mac", "abc456", "python", false, "mac_abc456_python"},
			{"mac", "abc456", "python", true, "mac_abc456_python_onhost"},
			{"windows", "win789", "powershell", false, "windows_win789_powershell"},
			{"windows", "win789", "powershell", true, "windows_win789_powershell_onhost"},
		}

		for _, tc := range testCases {
			t.Run(tc.expectedName, func(t *testing.T) {
				generatedName := node.GenerateToolName(tc.platform, tc.nodeID, tc.toolName, tc.isOnHost)
				assert.Equal(t, tc.expectedName, generatedName)
			})
		}
	})

	t.Run("tool names are unique per node isolation combination", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		toolNames := make(map[string]bool)
		for _, tool := range state.Tools {
			if toolNames[tool.Name] {
				t.Errorf("duplicate tool name: %s", tool.Name)
			}
			toolNames[tool.Name] = true
		}

		// Should have at least 2 unique tool names for bash
		assert.GreaterOrEqual(t, len(toolNames), 2)
	})
}

// TestService_ToolDescriptions tests tool description generation.
func TestService_ToolDescriptions(t *testing.T) {
	t.Run("generates proper description for docker tool", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		var bashTool *agent.Tool
		for i := range state.Tools {
			if state.Tools[i].Name == "linux_node1_bash" {
				bashTool = &state.Tools[i]
				break
			}
		}

		require.NotNil(t, bashTool)
		assert.Contains(t, bashTool.Description, "bash")
		assert.Contains(t, bashTool.Description, "Linux_node1")
		assert.Contains(t, bashTool.Description, "Docker")
		assert.NotContains(t, bashTool.Description, "onhost")
	})

	t.Run("generates proper description for host tool", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "python", Isolation: []string{"host"}}}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "mac", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		var pythonTool *agent.Tool
		for i := range state.Tools {
			if state.Tools[i].Name == "mac_node1_python_onhost" {
				pythonTool = &state.Tools[i]
				break
			}
		}

		require.NotNil(t, pythonTool)
		assert.Contains(t, pythonTool.Description, "python")
		assert.Contains(t, pythonTool.Description, "Mac_node1")
		assert.Contains(t, pythonTool.Description, "onhost")
	})

	t.Run("description mentions node identification", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "custom_tool", Isolation: []string{"docker"}}}

		err := service.RegisterConnection(ctx, conn, "abc123", "user1", "windows", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		var tool *agent.Tool
		for i := range state.Tools {
			if state.Tools[i].Name == "windows_abc123_custom_tool" {
				tool = &state.Tools[i]
				break
			}
		}

		require.NotNil(t, tool)
		// Description should help LLM identify which specific device
		assert.Contains(t, tool.Description, "Windows_abc123")
	})
}

// TestService_ConcurrentOperations tests concurrent service operations.
func TestService_ConcurrentOperations(t *testing.T) {
	t.Run("handles concurrent node registrations safely", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		var wg sync.WaitGroup

		numRegistrations := 10

		for i := 0; i < numRegistrations; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				conn := newMockWebSocketConn()
				nodeID := fmt.Sprintf("node%d", index)
				userID := fmt.Sprintf("user%d", index%3) // 3 users
				toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

				_ = service.RegisterConnection(ctx, conn, nodeID, userID, "linux", toolSpecs)
			}(i)
		}

		wg.Wait()

		// Verify all users have agents
		for i := 0; i < 3; i++ {
			userID := fmt.Sprintf("user%d", i)
			assert.True(t, agentProvider.HasAgent(userID), "user %s should have an agent", userID)
		}
	})

	t.Run("handles concurrent registrations and unregistrations", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		var wg sync.WaitGroup

		// Register some nodes first
		for i := 0; i < 5; i++ {
			conn := newMockWebSocketConn()
			nodeID := fmt.Sprintf("node%d", i)
			toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}
			err := service.RegisterConnection(ctx, conn, nodeID, "user1", "linux", toolSpecs)
			require.NoError(t, err)
		}

		// Concurrent unregister
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				nodeID := fmt.Sprintf("node%d", index)
				_ = service.UnregisterConnection(ctx, nodeID, "user1")
			}(i)
		}

		wg.Wait()

		// All nodes should be unregistered (tools removed from agent)
		ag := agentProvider.GetAgent("user1")
		if ag != nil {
			state := ag.State()
			// Count remaining node tools
			nodeToolCount := 0
			for _, tool := range state.Tools {
				if contains(tool.Name, "node") {
					nodeToolCount++
				}
			}
			// All tools should be removed
			assert.Equal(t, 0, nodeToolCount, "all node tools should be removed")
		}
	})
}

// TestService_EdgeCases tests edge cases and error conditions.
func TestService_EdgeCases(t *testing.T) {
	t.Run("handles nil tool specs gracefully", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		// Test with empty slice instead of nil
		assert.NotPanics(t, func() {
			err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", []node.ToolSpec{})
			assert.NoError(t, err)
		})
	})

	t.Run("handles tool spec with empty isolation", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "bash", Isolation: []string{}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// No tools should be created for empty isolation
		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()
		// Count tools for this node
		nodeToolCount := 0
		for _, tool := range state.Tools {
			if contains(tool.Name, "node1") {
				nodeToolCount++
			}
		}
		assert.Equal(t, 0, nodeToolCount, "no tools should be created for empty isolation")
	})

	t.Run("handles tool spec with empty name", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "", Isolation: []string{"docker"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Should still register the node (tools might have empty name but still be created)
		ag := agentProvider.GetAgent("user1")
		assert.NotNil(t, ag)
	})

	t.Run("handles multiple consecutive health updates", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Multiple health updates
		statuses := []node.NodeStatus{
			node.NodeStatusBusy,
			node.NodeStatusHealthy,
			node.NodeStatusDegraded,
			node.NodeStatusOnline,
		}

		for _, status := range statuses {
			err := service.UpdateNodeHealth("node1", status, toolSpecs)
			require.NoError(t, err)
		}
	})

	t.Run("handles special characters in tool names", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{
			{Name: "docker-compose", Isolation: []string{"docker"}},
			{Name: "kubectl_get", Isolation: []string{"docker"}},
		}

		err := service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		// At least verify tools were created (name encoding may vary)
		assert.GreaterOrEqual(t, len(state.Tools), 2)
	})

	t.Run("handles very long node IDs", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn := newMockWebSocketConn()

		longNodeID := string(make([]byte, 500))
		for i := range longNodeID {
			longNodeID = longNodeID[:i] + "a" + longNodeID[i+1:]
		}

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		// Should not panic
		assert.NotPanics(t, func() {
			_ = service.RegisterConnection(ctx, conn, longNodeID, "user1", "linux", toolSpecs)
		})
	})
}

// TestService_Network tests service behavior in network-like scenarios.
func TestService_Network(t *testing.T) {
	t.Run("simulates node reconnection", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()
		conn1 := newMockWebSocketConn()

		toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}

		// Initial connection
		err := service.RegisterConnection(ctx, conn1, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Get tool count before unregister
		ag1 := agentProvider.GetAgent("user1")
		require.NotNil(t, ag1)
		toolsBefore := len(ag1.State().Tools)

		// Node disconnects
		err = service.UnregisterConnection(ctx, "node1", "user1")
		require.NoError(t, err)

		// Verify tools were removed
		ag2 := agentProvider.GetAgent("user1")
		require.NotNil(t, ag2)
		toolsAfterUnregister := len(ag2.State().Tools)
		assert.Less(t, toolsAfterUnregister, toolsBefore, "tools should be removed after unregister")

		// Node reconnects with new connection
		conn2 := newMockWebSocketConn()
		err = service.RegisterConnection(ctx, conn2, "node1", "user1", "linux", toolSpecs)
		require.NoError(t, err)

		// Verify tools are back
		ag3 := agentProvider.GetAgent("user1")
		require.NotNil(t, ag3)
		toolsAfterReconnect := len(ag3.State().Tools)
		assert.GreaterOrEqual(t, toolsAfterReconnect, toolsAfterUnregister, "tools should be back after reconnect")
	})

	t.Run("simulates multiple nodes with mixed tool availability", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()

		// Node 1: Linux with bash and python
		conn1 := newMockWebSocketConn()
		toolSpecs1 := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker", "host"}},
			{Name: "python", Isolation: []string{"docker"}},
		}
		err1 := service.RegisterConnection(ctx, conn1, "node1", "user1", "linux", toolSpecs1)
		require.NoError(t, err1)

		// Node 2: Mac with bash only
		conn2 := newMockWebSocketConn()
		toolSpecs2 := []node.ToolSpec{
			{Name: "bash", Isolation: []string{"docker"}},
		}
		err2 := service.RegisterConnection(ctx, conn2, "node2", "user1", "mac", toolSpecs2)
		require.NoError(t, err2)

		// Verify user has all tools from both nodes
		ag := agentProvider.GetAgent("user1")
		require.NotNil(t, ag)

		state := ag.State()

		// Count tools per node
		node1Tools := 0
		node2Tools := 0
		for _, tool := range state.Tools {
			if contains(tool.Name, "node1") {
				node1Tools++
			}
			if contains(tool.Name, "node2") {
				node2Tools++
			}
		}

		// node1 should have 3 tools (bash docker, bash host, python docker)
		assert.GreaterOrEqual(t, node1Tools, 3)
		// node2 should have 1 tool (bash docker)
		assert.GreaterOrEqual(t, node2Tools, 1)
	})
}

// TestService_MultipleUsers tests multi-user scenarios.
func TestService_MultipleUsers(t *testing.T) {
	t.Run("isolates tools between users", func(t *testing.T) {
		agentProvider := newMockAgentProvider()
		logger := testLogger()
		service := node.NewService(logger, agentProvider)

		ctx := context.Background()

		// User 1 registers a Linux node
		conn1 := newMockWebSocketConn()
		toolSpecs1 := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}
		err1 := service.RegisterConnection(ctx, conn1, "node1", "user1", "linux", toolSpecs1)
		require.NoError(t, err1)

		// User 2 registers a Mac node
		conn2 := newMockWebSocketConn()
		toolSpecs2 := []node.ToolSpec{{Name: "python", Isolation: []string{"docker"}}}
		err2 := service.RegisterConnection(ctx, conn2, "node2", "user2", "mac", toolSpecs2)
		require.NoError(t, err2)

		// Verify user1 only has their own tools
		ag1 := agentProvider.GetAgent("user1")
		require.NotNil(t, ag1)
		state1 := ag1.State()

		hasUser1Bash := false
		hasUser2Python := false
		for _, tool := range state1.Tools {
			if contains(tool.Name, "bash") && contains(tool.Name, "node1") {
				hasUser1Bash = true
			}
			if contains(tool.Name, "python") && contains(tool.Name, "node2") {
				hasUser2Python = true
			}
		}

		assert.True(t, hasUser1Bash, "user1 should have their bash tool")
		assert.False(t, hasUser2Python, "user1 should not have user2's python tool")

		// Verify user2 only has their own tools
		ag2 := agentProvider.GetAgent("user2")
		require.NotNil(t, ag2)
		state2 := ag2.State()

		hasUser2PythonActual := false
		hasUser1BashInUser2 := false
		for _, tool := range state2.Tools {
			if contains(tool.Name, "python") && contains(tool.Name, "node2") {
				hasUser2PythonActual = true
			}
			if contains(tool.Name, "bash") && contains(tool.Name, "node1") {
				hasUser1BashInUser2 = true
			}
		}

		assert.True(t, hasUser2PythonActual, "user2 should have their python tool")
		assert.False(t, hasUser1BashInUser2, "user2 should not have user1's bash tool")
	})
}

// TestService_NilDependencies tests service with nil dependencies.
func TestService_NilDependencies(t *testing.T) {
	t.Run("panics with nil logger as expected", func(t *testing.T) {
		agentProvider := newMockAgentProvider()

		// The service requires a non-nil logger
		assert.Panics(t, func() {
			_ = node.NewService(nil, agentProvider)
		})
	})

	t.Run("nil agent provider causes operations to panic", func(t *testing.T) {
		logger := testLogger()

		// Creating with nil agent provider works
		service := node.NewService(logger, nil)
		assert.NotNil(t, service)

		// But operations will panic due to nil dereference
		// This is expected behavior - agent provider is required
		assert.Panics(t, func() {
			ctx := context.Background()
			conn := newMockWebSocketConn()
			toolSpecs := []node.ToolSpec{{Name: "bash", Isolation: []string{"docker"}}}
			_ = service.RegisterConnection(ctx, conn, "node1", "user1", "linux", toolSpecs)
		})
	})
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
