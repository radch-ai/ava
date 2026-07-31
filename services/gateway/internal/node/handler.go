// Package node provides WebSocket handler for node connections.
package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	zitadelvalidate "github.com/nevaria-ai/zitadel-validate"
	"github.com/rs/zerolog"
)

var (
	// ErrNoToken is returned when no token is provided.
	ErrNoToken = errors.New("no token provided")
	// ErrInvalidToken is returned when token validation fails.
	ErrInvalidToken = errors.New("invalid token")
)

// Handler manages WebSocket connections from nodes.
type Handler struct {
	service     *Service
	authClient  AuthClient
	logger      *zerolog.Logger
	maxConns    int
	connSem     chan struct{}
	activeConns int
	connMu      sync.RWMutex
	acceptOpts  websocket.AcceptOptions
}

// AuthClient defines the interface for validating sessions.
type AuthClient interface {
	ValidateSession(ctx context.Context, token string) (*zitadelvalidate.SessionInfo, error)
}

// NewHandler creates a new node WebSocket handler.
func NewHandler(service *Service, authClient AuthClient, logger *zerolog.Logger, maxConns int) *Handler {
	return &Handler{
		service:    service,
		authClient: authClient,
		logger:     logger,
		maxConns:   maxConns,
		connSem:    make(chan struct{}, maxConns),
		acceptOpts: websocket.AcceptOptions{
			OriginPatterns: []string{"*"}, // TODO: Configure proper origin checking
		},
	}
}

// HandleNodeConnect handles WebSocket upgrade and connection for nodes.
//
// Expected query parameters:
//   - token: Authentication token (required)
//
// Expected messages from node:
//   - {"type": "register", "data": {"node_id": "...", "platform": "...", "tools": [...]}}
//   - {"type": "heartbeat", "data": {"tools": [...]}}
//   - {"type": "tool_result", "data": {"call_id": "...", "result": {...}, "error": "..."}}
func (h *Handler) HandleNodeConnect(w http.ResponseWriter, r *http.Request) {
	// 1. Check connection limit
	select {
	case h.connSem <- struct{}{}:
	default:
		h.logger.Warn().Msg("Node connection limit reached")
		http.Error(w, "Too many connections", http.StatusServiceUnavailable)
		return
	}

	// 2. Extract and validate token
	token := r.URL.Query().Get("token")
	if token == "" {
		h.releaseConn()
		http.Error(w, "Missing token", http.StatusUnauthorized)
		return
	}

	// 3. Validate session
	session, err := h.authClient.ValidateSession(r.Context(), token)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to validate node token")
		h.releaseConn()
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	userID := session.UserID
	h.logger.Info().Str("user_id", userID).Msg("Node connection authenticated")

	// 4. Upgrade to WebSocket
	conn, err := websocket.Accept(w, r, &h.acceptOpts)
	if err != nil {
		h.logger.Error().Err(err).Msg("Failed to upgrade WebSocket")
		h.releaseConn()
		return
	}

	// 5. Track active connection
	h.connMu.Lock()
	h.activeConns++
	h.connMu.Unlock()

	// 6. Handle connection (blocking)
	defer func() {
		_ = conn.CloseNow()
		h.releaseConn()
	}()

	// 7. Handle node protocol
	h.handleConnection(r.Context(), conn, userID)
}

// handleConnection handles the node protocol after WebSocket upgrade.
func (h *Handler) handleConnection(ctx context.Context, conn *websocket.Conn, userID string) {
	var nodeID string

	defer func() {
		// Unregister node on disconnect
		if nodeID != "" {
			err := h.service.UnregisterConnection(ctx, nodeID, userID)
			if err != nil {
				h.logger.Error().Err(err).Str("node_id", nodeID).Msg("Failed to unregister node")
			}
		}
	}()

	for {
		// Read message with timeout
		readCtx, readCancel := context.WithTimeout(ctx, 60*time.Second)
		typ, reader, err := conn.Reader(readCtx)
		if err != nil {
			readCancel()
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
				websocket.CloseStatus(err) == websocket.StatusGoingAway {
				h.logger.Info().Str("node_id", nodeID).Msg("Node connection closed")
			} else {
				h.logger.Error().Err(err).Str("node_id", nodeID).Msg("Error reading from node")
			}
			return
		}

		data, err := io.ReadAll(reader)
		readCancel()
		if err != nil {
			h.logger.Error().Err(err).Str("node_id", nodeID).Msg("Error reading message data")
			return
		}

		if typ != websocket.MessageText {
			continue
		}

		// Parse message
		var msg NodeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			h.logger.Error().Err(err).Msg("Failed to parse node message")
			continue
		}

		// Handle message type
		switch msg.Type {
		case "register":
			if err := h.handleRegister(ctx, conn, userID, &msg, &nodeID); err != nil {
				h.logger.Error().Err(err).Msg("Failed to handle register")
				return
			}

		case "heartbeat":
			if err := h.handleHeartbeat(ctx, nodeID, &msg); err != nil {
				h.logger.Error().Err(err).Msg("Failed to handle heartbeat")
			}

		case "tool_result":
			// Results are handled by the executor's listener
			// This is just a placeholder for logging
			h.logger.Debug().Str("node_id", nodeID).Msg("Received tool_result")

		default:
			h.logger.Warn().Str("type", msg.Type).Msg("Unknown node message type")
		}
	}
}

// handleRegister handles a register message from a node.
func (h *Handler) handleRegister(ctx context.Context, conn *websocket.Conn, userID string, msg *NodeMessage, nodeID *string) error {
	var data RegisterData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return fmt.Errorf("failed to parse register data: %w", err)
	}

	// Validate required fields
	if data.NodeID == "" {
		return errors.New("missing node_id")
	}
	if data.Platform == "" {
		return errors.New("missing platform")
	}

	// Register connection and tools with service
	err := h.service.RegisterConnection(ctx, conn, data.NodeID, userID, data.Platform, data.Tools)
	if err != nil {
		return fmt.Errorf("failed to register connection: %w", err)
	}

	*nodeID = data.NodeID

	// Send registration confirmation
	response := map[string]any{
		"type": "registered",
		"data": map[string]any{
			"node_id": data.NodeID,
		},
	}
	respData, _ := json.Marshal(response)
	writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
	_ = conn.Write(writeCtx, websocket.MessageText, respData)
	writeCancel()

	// Convert tool specs to names for logging
	toolNames := make([]string, 0, len(data.Tools))
	for _, spec := range data.Tools {
		toolNames = append(toolNames, spec.Name)
	}

	h.logger.Info().
		Str("node_id", data.NodeID).
		Str("platform", data.Platform).
		Strs("tools", toolNames).
		Msg("Node registered")

	return nil
}

// handleHeartbeat handles a heartbeat message from a node.
func (h *Handler) handleHeartbeat(ctx context.Context, nodeID string, msg *NodeMessage) error {
	var data HeartbeatData
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return fmt.Errorf("failed to parse heartbeat data: %w", err)
	}

	// Update node health (service handles ToolSpec conversion)
	err := h.service.UpdateNodeHealth(nodeID, NodeStatusOnline, data.Tools)
	if err != nil {
		return fmt.Errorf("failed to update health: %w", err)
	}

	return nil
}

// releaseConn releases a connection slot.
func (h *Handler) releaseConn() {
	h.connMu.Lock()
	h.activeConns--
	h.connMu.Unlock()
	<-h.connSem
}

// ActiveConns returns the number of active connections.
func (h *Handler) ActiveConns() int {
	h.connMu.RLock()
	defer h.connMu.RUnlock()
	return h.activeConns
}

// NodeMessage represents a message sent between node and gateway.
type NodeMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// RegisterData is the data for a register message.
type RegisterData struct {
	NodeID   string     `json:"node_id"`
	Platform string     `json:"platform"`
	Tools    []ToolSpec `json:"tools"`
}

// HeartbeatData is the data for a heartbeat message.
type HeartbeatData struct {
	Tools []ToolSpec `json:"tools"`
}
