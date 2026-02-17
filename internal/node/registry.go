package node

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrNodeAlreadyRegistered is returned when attempting to register a node with an existing ID.
	ErrNodeAlreadyRegistered = errors.New("node already registered")
	// ErrNodeNotFound is returned when attempting to operate on a non-existent node.
	ErrNodeNotFound = errors.New("node not found")
)

// Registry manages connected nodes in a thread-safe manner.
type Registry struct {
	mu              sync.RWMutex
	nodes           map[string]*NodeInfo       // nodeID -> NodeInfo
	nodeConnections map[string]*NodeConnection // nodeID -> NodeConnection (for executor)
	connections     map[string]WebSocketConn   // nodeID -> connection
	userNodes       map[string][]string        // userID -> []nodeID
	staleTimeout    time.Duration
}

// NewRegistry creates a new empty node registry.
func NewRegistry() *Registry {
	return &Registry{
		nodes:           make(map[string]*NodeInfo),
		connections:     make(map[string]WebSocketConn),
		nodeConnections: make(map[string]*NodeConnection),
		userNodes:       make(map[string][]string),
		staleTimeout:    5 * time.Minute, // Nodes are stale after 5 minutes of no updates
	}
}

// RegisterNode registers a new node with the registry.
func (r *Registry) RegisterNode(ctx context.Context, userID, nodeID, platform string, tools []string, conn WebSocketConn) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.nodes[nodeID]; exists {
		return fmt.Errorf("%w: node %s", ErrNodeAlreadyRegistered, nodeID)
	}

	now := time.Now()
	nodeInfo := &NodeInfo{
		NodeID:      nodeID,
		Platform:    platform,
		UserID:      userID,
		ConnectedAt: now,
		LastSeenAt:  now,
		LastSeen:    now, // Set alias
		Tools:       tools,
		Status:      NodeStatusOnline,
	}

	r.nodes[nodeID] = nodeInfo
	r.connections[nodeID] = conn
	r.userNodes[userID] = append(r.userNodes[userID], nodeID)

	return nil
}

// UnregisterNode removes a node from the registry.
func (r *Registry) UnregisterNode(ctx context.Context, nodeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	nodeInfo, exists := r.nodes[nodeID]
	if !exists {
		return nil // Idempotent - no error if node doesn't exist
	}

	// Remove from nodes map
	delete(r.nodes, nodeID)

	// Remove from connections map
	delete(r.connections, nodeID)

	// Remove from userNodes map
	userID := nodeInfo.UserID
	if nodeIDs, ok := r.userNodes[userID]; ok {
		// Filter out the nodeID
		newNodeIDs := make([]string, 0, len(nodeIDs)-1)
		for _, id := range nodeIDs {
			if id != nodeID {
				newNodeIDs = append(newNodeIDs, id)
			}
		}
		if len(newNodeIDs) == 0 {
			delete(r.userNodes, userID)
		} else {
			r.userNodes[userID] = newNodeIDs
		}
	}

	return nil
}

// GetNode returns the node info for the given node ID.
// Returns nil if the node doesn't exist.
func (r *Registry) GetNode(nodeID string) *NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.nodes[nodeID]
}

// GetNodeInfo returns a copy of the node info for the given node ID along with existence flag.
// This is for registry test compatibility.
func (r *Registry) GetNodeInfo(nodeID string) (NodeInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodeInfo, exists := r.nodes[nodeID]
	if !exists {
		return NodeInfo{}, false
	}

	return *nodeInfo, true
}

// GetUserNodes returns all nodes for a given user ID.
func (r *Registry) GetUserNodes(userID string) []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodeIDs, exists := r.userNodes[userID]
	if !exists {
		return []NodeInfo{}
	}

	nodes := make([]NodeInfo, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		if nodeInfo, ok := r.nodes[nodeID]; ok {
			nodes = append(nodes, *nodeInfo)
		}
	}

	return nodes
}

// GetNodeConnection returns the WebSocket connection for a node.
func (r *Registry) GetNodeConnection(nodeID string) (WebSocketConn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conn, exists := r.connections[nodeID]
	return conn, exists
}

// UpdateNodeHealth updates the status, tools, and last seen timestamp for a node.
func (r *Registry) UpdateNodeHealth(nodeID string, status NodeStatus, tools []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	nodeInfo, exists := r.nodes[nodeID]
	if !exists {
		return fmt.Errorf("%w: node %s", ErrNodeNotFound, nodeID)
	}

	nodeInfo.Status = status
	nodeInfo.Tools = tools
	now := time.Now()
	nodeInfo.LastSeenAt = now
	nodeInfo.LastSeen = now // Update alias as well

	return nil
}

// IsNodeOnline checks if a node is online (not offline and not stale).
func (r *Registry) IsNodeOnline(nodeID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodeInfo, exists := r.nodes[nodeID]
	if !exists {
		return false
	}

	// Check if node is offline
	if nodeInfo.Status == NodeStatusOffline {
		return false
	}

	// Check if node is stale (no health update for staleTimeout duration)
	if time.Since(nodeInfo.LastSeenAt) > r.staleTimeout {
		return false
	}

	return true
}

// GetNodesByTool returns all nodes that have a specific tool.
func (r *Registry) GetNodesByTool(tool string) []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var nodes []NodeInfo
	for _, nodeInfo := range r.nodes {
		// Check if node has the tool
		for _, nodeTool := range nodeInfo.Tools {
			if nodeTool == tool {
				nodes = append(nodes, *nodeInfo)
				break
			}
		}
	}

	return nodes
}

// GetAllNodes returns all registered nodes.
func (r *Registry) GetAllNodes() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]NodeInfo, 0, len(r.nodes))
	for _, nodeInfo := range r.nodes {
		nodes = append(nodes, *nodeInfo)
	}

	return nodes
}

// RegisterConnection registers a node connection for executor use.
func (r *Registry) RegisterConnection(node *NodeConnection) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nodeConnections == nil {
		r.nodeConnections = make(map[string]*NodeConnection)
	}
	r.nodeConnections[node.NodeUUID] = node
}

// GetConnection retrieves a node connection for executor use.
func (r *Registry) GetConnection(nodeID string) (*NodeConnection, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.nodeConnections == nil {
		return nil, false
	}

	conn, exists := r.nodeConnections[nodeID]
	return conn, exists
}

// SetLastSeenAtForTest is a test helper that directly sets a node's LastSeenAt timestamp.
// This is exported for testing purposes only.
func (r *Registry) SetLastSeenAtForTest(nodeID string, t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if nodeInfo, exists := r.nodes[nodeID]; exists {
		nodeInfo.LastSeenAt = t
	}
}
