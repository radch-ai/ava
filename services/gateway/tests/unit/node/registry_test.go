// Package node_test provides unit tests for the node registry.
package node_test

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/radch-ai/ava/services/gateway/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRegistry verifies registry initialization.
func TestNewRegistry(t *testing.T) {
	registry := node.NewRegistry()

	assert.NotNil(t, registry)
	assert.NotNil(t, registry.GetAllNodes())
	assert.Empty(t, registry.GetAllNodes(), "new registry should have no nodes")
}

// TestRegistry_RegisterNode tests node registration.
func TestRegistry_RegisterNode(t *testing.T) {
	t.Run("successfully registers a new node", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		// Pass nil for conn since the registry test only tests registry logic
		// and the connection is stored but not used in these tests
		err := registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash", "python"}, nil)

		require.NoError(t, err)
		nodes := registry.GetAllNodes()
		assert.Len(t, nodes, 1)
		assert.Equal(t, "node1", nodes[0].NodeID)
		assert.Equal(t, "user1", nodes[0].UserID)
		assert.Equal(t, "linux", nodes[0].Platform)
		assert.Equal(t, []string{"bash", "python"}, nodes[0].Tools)
		assert.Equal(t, node.NodeStatusOnline, nodes[0].Status)
	})

	t.Run("returns error when registering duplicate node", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		err := registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)
		require.NoError(t, err)

		err = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"python"}, nil)
		assert.Error(t, err)
		assert.ErrorIs(t, err, node.ErrNodeAlreadyRegistered)
	})

	t.Run("registers multiple nodes for different users", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		err1 := registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)
		err2 := registry.RegisterNode(ctx, "user2", "node2", "mac", []string{"python"}, nil)

		require.NoError(t, err1)
		require.NoError(t, err2)

		nodes := registry.GetAllNodes()
		assert.Len(t, nodes, 2)
	})

	t.Run("registers multiple nodes for same user", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		err1 := registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)
		err2 := registry.RegisterNode(ctx, "user1", "node2", "mac", []string{"python"}, nil)

		require.NoError(t, err1)
		require.NoError(t, err2)

		userNodes := registry.GetUserNodes("user1")
		assert.Len(t, userNodes, 2)
	})
}

// TestRegistry_UnregisterNode tests node unregistration.
func TestRegistry_UnregisterNode(t *testing.T) {
	t.Run("successfully unregisters existing node", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)
		assert.Len(t, registry.GetAllNodes(), 1)

		err := registry.UnregisterNode(ctx, "node1")
		require.NoError(t, err)

		assert.Empty(t, registry.GetAllNodes())
		assert.Empty(t, registry.GetUserNodes("user1"))
	})

	t.Run("unregister is idempotent for non-existent nodes", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		err := registry.UnregisterNode(ctx, "nonexistent")
		assert.NoError(t, err, "unregister should not error for non-existent nodes")
	})

	t.Run("removes node from userNodes mapping", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)
		_ = registry.RegisterNode(ctx, "user1", "node2", "mac", []string{"python"}, nil)

		assert.Len(t, registry.GetUserNodes("user1"), 2)

		_ = registry.UnregisterNode(ctx, "node1")

		userNodes := registry.GetUserNodes("user1")
		assert.Len(t, userNodes, 1)
		assert.Equal(t, "node2", userNodes[0].NodeID)
	})

	t.Run("removes user entry when last node is unregistered", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)
		_ = registry.UnregisterNode(ctx, "node1")

		userNodes := registry.GetUserNodes("user1")
		assert.Empty(t, userNodes)
	})
}

// TestRegistry_GetNode tests retrieving node information.
func TestRegistry_GetNode(t *testing.T) {
	t.Run("returns nil for non-existent node", func(t *testing.T) {
		registry := node.NewRegistry()

		nodeInfo := registry.GetNode("nonexistent")
		assert.Nil(t, nodeInfo)
	})

	t.Run("returns correct node info", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash", "python"}, nil)

		nodeInfo := registry.GetNode("node1")
		require.NotNil(t, nodeInfo)

		assert.Equal(t, "node1", nodeInfo.NodeID)
		assert.Equal(t, "user1", nodeInfo.UserID)
		assert.Equal(t, "linux", nodeInfo.Platform)
		assert.Equal(t, []string{"bash", "python"}, nodeInfo.Tools)
		assert.Equal(t, node.NodeStatusOnline, nodeInfo.Status)
		assert.False(t, nodeInfo.ConnectedAt.IsZero())
		assert.False(t, nodeInfo.LastSeenAt.IsZero())
	})
}

// TestRegistry_GetNodeInfo tests retrieving node info with existence flag.
func TestRegistry_GetNodeInfo(t *testing.T) {
	t.Run("returns false for non-existent node", func(t *testing.T) {
		registry := node.NewRegistry()

		nodeInfo, exists := registry.GetNodeInfo("nonexistent")
		assert.False(t, exists)
		assert.Empty(t, nodeInfo.NodeID)
	})

	t.Run("returns copy of node info", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)

		nodeInfo, exists := registry.GetNodeInfo("node1")
		assert.True(t, exists)
		assert.Equal(t, "node1", nodeInfo.NodeID)
		assert.Equal(t, "user1", nodeInfo.UserID)
	})
}

// TestRegistry_GetUserNodes tests retrieving nodes by user.
func TestRegistry_GetUserNodes(t *testing.T) {
	t.Run("returns empty slice for user with no nodes", func(t *testing.T) {
		registry := node.NewRegistry()

		nodes := registry.GetUserNodes("user1")
		assert.Empty(t, nodes)
	})

	t.Run("returns all nodes for a user", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)
		_ = registry.RegisterNode(ctx, "user1", "node2", "mac", []string{"python"}, nil)
		_ = registry.RegisterNode(ctx, "user2", "node3", "windows", []string{"powershell"}, nil)

		user1Nodes := registry.GetUserNodes("user1")
		assert.Len(t, user1Nodes, 2)

		user2Nodes := registry.GetUserNodes("user2")
		assert.Len(t, user2Nodes, 1)
	})
}

// TestRegistry_GetNodeConnection tests retrieving WebSocket connections.
func TestRegistry_GetNodeConnection(t *testing.T) {
	t.Run("returns false for non-existent node", func(t *testing.T) {
		registry := node.NewRegistry()

		_, exists := registry.GetNodeConnection("nonexistent")
		assert.False(t, exists)
	})

	t.Run("returns correct connection", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		// Create a mock connection to store
		mockConn := &mockRegistryWebSocketConn{}

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, mockConn)

		retrievedConn, exists := registry.GetNodeConnection("node1")
		assert.True(t, exists)
		assert.Same(t, mockConn, retrievedConn)
	})
}

// TestRegistry_UpdateNodeHealth tests updating node health information.
func TestRegistry_UpdateNodeHealth(t *testing.T) {
	t.Run("successfully updates node health", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)

		originalLastSeen := registry.GetNode("node1").LastSeenAt
		time.Sleep(10 * time.Millisecond)

		err := registry.UpdateNodeHealth("node1", node.NodeStatusBusy, []string{"bash", "python"})

		require.NoError(t, err)

		nodeInfo := registry.GetNode("node1")
		assert.Equal(t, node.NodeStatusBusy, nodeInfo.Status)
		assert.Equal(t, []string{"bash", "python"}, nodeInfo.Tools)
		assert.True(t, nodeInfo.LastSeenAt.After(originalLastSeen))
	})

	t.Run("returns error for non-existent node", func(t *testing.T) {
		registry := node.NewRegistry()

		err := registry.UpdateNodeHealth("nonexistent", node.NodeStatusOnline, []string{})
		assert.Error(t, err)
		assert.ErrorIs(t, err, node.ErrNodeNotFound)
	})
}

// TestRegistry_IsNodeOnline tests checking if a node is online.
func TestRegistry_IsNodeOnline(t *testing.T) {
	t.Run("returns false for non-existent node", func(t *testing.T) {
		registry := node.NewRegistry()

		assert.False(t, registry.IsNodeOnline("nonexistent"))
	})

	t.Run("returns true for newly registered online node", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)

		assert.True(t, registry.IsNodeOnline("node1"))
	})

	t.Run("returns false for offline node", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)
		_ = registry.UpdateNodeHealth("node1", node.NodeStatusOffline, []string{})

		assert.False(t, registry.IsNodeOnline("node1"))
	})

	t.Run("returns false for stale node", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)

		// Use the test helper to set LastSeenAt to the past
		registry.SetLastSeenAtForTest("node1", time.Now().Add(-10*time.Minute))

		assert.False(t, registry.IsNodeOnline("node1"))
	})
}

// TestRegistry_GetNodesByTool tests finding nodes by tool availability.
func TestRegistry_GetNodesByTool(t *testing.T) {
	t.Run("returns empty slice for non-existent tool", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)

		nodes := registry.GetNodesByTool("python")
		assert.Empty(t, nodes)
	})

	t.Run("returns nodes that have the specified tool", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash", "python"}, nil)
		_ = registry.RegisterNode(ctx, "user1", "node2", "mac", []string{"bash"}, nil)
		_ = registry.RegisterNode(ctx, "user2", "node3", "windows", []string{"python"}, nil)

		pythonNodes := registry.GetNodesByTool("python")
		assert.Len(t, pythonNodes, 2)

		nodeIDs := make([]string, len(pythonNodes))
		for i, n := range pythonNodes {
			nodeIDs[i] = n.NodeID
		}
		assert.Contains(t, nodeIDs, "node1")
		assert.Contains(t, nodeIDs, "node3")
	})
}

// TestRegistry_GetAllNodes tests retrieving all nodes.
func TestRegistry_GetAllNodes(t *testing.T) {
	t.Run("returns empty slice when no nodes registered", func(t *testing.T) {
		registry := node.NewRegistry()
		assert.Empty(t, registry.GetAllNodes())
	})

	t.Run("returns all registered nodes", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)
		_ = registry.RegisterNode(ctx, "user2", "node2", "mac", []string{"python"}, nil)

		nodes := registry.GetAllNodes()
		assert.Len(t, nodes, 2)
	})

	t.Run("returns copies not references to internal state", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()

		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, nil)

		nodes1 := registry.GetAllNodes()
		nodes2 := registry.GetAllNodes()

		// Modify returned slice
		nodes1[0].NodeID = "modified"

		// Second call should not reflect the modification
		assert.Equal(t, "node1", nodes2[0].NodeID)
	})
}

// TestRegistry_RegisterConnection tests executor connection registration.
func TestRegistry_RegisterConnection(t *testing.T) {
	t.Run("registers node connection for executor", func(t *testing.T) {
		registry := node.NewRegistry()
		mockConn := &mockRegistryWebSocketConn{}

		nodeConn := &node.NodeConnection{
			NodeUUID: "node1",
			Conn:     mockConn,
			SendCh:   make(chan []byte, 10),
		}

		registry.RegisterConnection(nodeConn)

		conn, exists := registry.GetConnection("node1")
		assert.True(t, exists)
		assert.Same(t, mockConn, conn.Conn)
		assert.Equal(t, "node1", conn.NodeUUID)
	})

	t.Run("allows overwriting existing connection", func(t *testing.T) {
		registry := node.NewRegistry()
		conn1 := &mockRegistryWebSocketConn{}
		conn2 := &mockRegistryWebSocketConn{}

		nodeConn1 := &node.NodeConnection{
			NodeUUID: "node1",
			Conn:     conn1,
			SendCh:   make(chan []byte, 10),
		}

		nodeConn2 := &node.NodeConnection{
			NodeUUID: "node1",
			Conn:     conn2,
			SendCh:   make(chan []byte, 10),
		}

		registry.RegisterConnection(nodeConn1)
		registry.RegisterConnection(nodeConn2)

		conn, exists := registry.GetConnection("node1")
		assert.True(t, exists)
		assert.Same(t, conn2, conn.Conn)
	})
}

// TestRegistry_GetConnection tests retrieving executor connections.
func TestRegistry_GetConnection(t *testing.T) {
	t.Run("returns false for non-existent connection", func(t *testing.T) {
		registry := node.NewRegistry()

		_, exists := registry.GetConnection("nonexistent")
		assert.False(t, exists)
	})

	t.Run("returns registered connection", func(t *testing.T) {
		registry := node.NewRegistry()
		mockConn := &mockRegistryWebSocketConn{}

		nodeConn := &node.NodeConnection{
			NodeUUID: "node1",
			Conn:     mockConn,
			SendCh:   make(chan []byte, 10),
		}

		registry.RegisterConnection(nodeConn)

		conn, exists := registry.GetConnection("node1")
		assert.True(t, exists)
		assert.Equal(t, "node1", conn.NodeUUID)
		assert.Same(t, mockConn, conn.Conn)
	})
}

// TestRegistry_ConcurrentAccess tests thread-safety of registry operations.
func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Run("handles concurrent registrations safely", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()
		var wg sync.WaitGroup

		numGoroutines := 100
		nodesPerGoroutine := 10

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for j := 0; j < nodesPerGoroutine; j++ {
					nodeID := fmt.Sprintf("node_%d_%d", goroutineID, j)
					userID := fmt.Sprintf("user_%d", goroutineID)
					_ = registry.RegisterNode(ctx, userID, nodeID, "linux", []string{"bash"}, nil)
				}
			}(i)
		}

		wg.Wait()

		// Should have numGoroutines * nodesPerGoroutine nodes
		allNodes := registry.GetAllNodes()
		assert.Len(t, allNodes, numGoroutines*nodesPerGoroutine)
	})

	t.Run("handles concurrent reads and writes", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()
		var wg sync.WaitGroup

		// Register initial nodes
		for i := 0; i < 10; i++ {
			nodeID := fmt.Sprintf("node_%d", i)
			_ = registry.RegisterNode(ctx, "user1", nodeID, "linux", []string{"bash"}, nil)
		}

		// Concurrent reads
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = registry.GetNode("node_1")
				_ = registry.GetUserNodes("user1")
				_ = registry.GetAllNodes()
			}()
		}

		// Concurrent writes
		for i := 10; i < 20; i++ {
			wg.Add(1)
			go func(nodeID int) {
				defer wg.Done()
				id := fmt.Sprintf("node_%d", nodeID)
				_ = registry.RegisterNode(ctx, "user2", id, "mac", []string{"python"}, nil)
			}(i)
		}

		wg.Wait()

		// Verify final state
		allNodes := registry.GetAllNodes()
		assert.Len(t, allNodes, 20)
	})
}

// TestRegistry_ConnectionMapping tests that connection maps stay in sync.
func TestRegistry_ConnectionMapping(t *testing.T) {
	t.Run("nodeConnections and connections stay in sync", func(t *testing.T) {
		registry := node.NewRegistry()
		ctx := context.Background()
		wsConn := &mockRegistryWebSocketConn{}

		// Register node with websocket conn
		_ = registry.RegisterNode(ctx, "user1", "node1", "linux", []string{"bash"}, wsConn)

		// Register NodeConnection for executor
		nodeConn := &node.NodeConnection{
			NodeUUID: "node1",
			Conn:     wsConn,
			SendCh:   make(chan []byte, 10),
		}
		registry.RegisterConnection(nodeConn)

		// Both should be accessible
		wsRetrieved, wsExists := registry.GetNodeConnection("node1")
		assert.True(t, wsExists)
		assert.Same(t, wsConn, wsRetrieved)

		execRetrieved, execExists := registry.GetConnection("node1")
		assert.True(t, execExists)
		assert.Equal(t, "node1", execRetrieved.NodeUUID)
		assert.Same(t, wsConn, execRetrieved.Conn)

		// Unregister should clean up both
		_ = registry.UnregisterNode(ctx, "node1")

		_, wsExistsAfter := registry.GetNodeConnection("node1")
		assert.False(t, wsExistsAfter, "WebSocket connection should be removed")

		// Note: executor connection is not removed by UnregisterNode
		// This is intentional as the executor may still need to communicate
	})
}

// mockRegistryWebSocketConn is a mock WebSocket connection for testing.
type mockRegistryWebSocketConn struct{}

func (m *mockRegistryWebSocketConn) Write(ctx context.Context, typ coderws.MessageType, p []byte) error {
	return nil
}

func (m *mockRegistryWebSocketConn) Reader(ctx context.Context) (coderws.MessageType, io.Reader, error) {
	return 0, nil, context.Canceled
}

func (m *mockRegistryWebSocketConn) CloseNow() error {
	return nil
}
