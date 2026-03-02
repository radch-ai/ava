// Package websocket provides WebSocket connection handling with multi-agent routing.
package websocket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
	"github.com/nevaria-ai/gateway/internal/agent"
	internalctx "github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/rs/zerolog"
)

// Connection configuration constants.
const (
	// MaxMessageSize limits message size to 64KB to prevent memory exhaustion attacks.
	MaxMessageSize = 65536

	// readTimeout is the deadline for reading from WebSocket (60s).
	readTimeout = 60 * time.Second

	// writeTimeout is the deadline for writing to WebSocket (10s).
	writeTimeout = 10 * time.Second

	// pingInterval is how often ping frames are sent (30s).
	pingInterval = 30 * time.Second
)

// Handler manages WebSocket connections with connection limiting and origin validation.
type Handler struct {
	logger       zerolog.Logger
	acceptOpts   websocket.AcceptOptions
	connSem      chan struct{} // Semaphore for connection limiting
	maxConns     int
	activeConns  int32 // atomic counter
	agentManager *AgentManager
}

// NewHandler creates a new WebSocket handler with connection limiting.
func NewHandler(logger zerolog.Logger, allowedOrigins []string, maxConns int, agentOpts *AgentManagerOptions) *Handler {
	acceptOpts := websocket.AcceptOptions{
		OriginPatterns: allowedOrigins,
	}

	sem := make(chan struct{}, maxConns)

	return &Handler{
		logger:       logger,
		acceptOpts:   acceptOpts,
		connSem:      sem,
		maxConns:     maxConns,
		agentManager: NewAgentManager(logger, agentOpts),
	}
}

// HandleWebSocket handles an incoming WebSocket connection upgrade request.
// Each connection spawns one goroutine for ping loop; reads happen on main goroutine.
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check connection limit
	if h.connSem != nil {
		select {
		case h.connSem <- struct{}{}:
			// Got slot, continue
			defer func() { <-h.connSem }()
		default:
			// At capacity
			h.logger.Warn().Int("limit", h.maxConns).Msg("connection limit reached")
			http.Error(w, "Server at capacity", http.StatusServiceUnavailable)
			return
		}
	}

	// Increment active connection counter
	atomic.AddInt32(&h.activeConns, 1)
	defer atomic.AddInt32(&h.activeConns, -1)

	// Upgrade to WebSocket
	conn, err := websocket.Accept(w, r, &h.acceptOpts)
	if err != nil {
		h.logger.Error().Err(err).Msg("websocket upgrade failed")
		return
	}

	// Ensure connection is closed on all exit paths
	defer func() { _ = conn.CloseNow() }()

	// Set max message size to prevent memory exhaustion attacks
	conn.SetReadLimit(MaxMessageSize)

	// Generate unique connection ID
	connID := uuid.New().String()

	// Get user_id from context (set by auth middleware)
	userID := internalctx.GetUserIDFromContext(r.Context())

	// Get real client IP (behind proxy)
	clientIP := r.Header.Get("X-Forwarded-For")
	if clientIP == "" {
		clientIP = r.Header.Get("X-Real-IP")
	}
	if clientIP == "" {
		h.logger.Warn().Str("real_ip", r.RemoteAddr).Msg("no proxy headers found, using direct connection (possibly proxy IP)")
		clientIP = r.RemoteAddr
	}

	logCtx := h.logger.With().
		Str("conn_id", connID).
		Str("user_id", userID).
		Str("client_ip", clientIP).
		Int("active_conns", int(atomic.LoadInt32(&h.activeConns))).
		Logger()

	logCtx.Info().Msg("websocket connection established")

	// Context for graceful shutdown - cancellable by any goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Watch for client disconnect
	go func() {
		<-r.Context().Done()
		cancel()
	}()

	// Start ping goroutine to keep connection alive
	go h.pingLoop(ctx, conn, logCtx, cancel)

	// Send connected event to client
	connectedEvent := wsEvent{
		Type: "connected",
		Data: map[string]string{
			"user_id": userID,
			"conn_id": connID,
		},
	}
	writeCtx, writeCancel := context.WithTimeout(ctx, writeTimeout)
	if err := wsjson.Write(writeCtx, conn, connectedEvent); err != nil {
		writeCancel()
		logCtx.Warn().Err(err).Msg("failed to send connected event")
		return
	}
	writeCancel()

	// Create connection wrapper for agent streaming
	connWrapper := NewWSConnWrapper(conn)

	// Message loop
	for {
		select {
		case <-ctx.Done():
			logCtx.Info().Msg("context cancelled, closing connection")
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		default:
			// Set read deadline
			readCtx, readCancel := context.WithTimeout(ctx, readTimeout)

			// Read message
			typ, reader, err := conn.Reader(readCtx)
			if err != nil {
				readCancel()
				if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
					websocket.CloseStatus(err) == websocket.StatusGoingAway {
					logCtx.Info().Msg("client closed connection")
				} else {
					logCtx.Error().Err(err).Msg("read error")
				}
				cancel()
				return
			}

			// Read the complete message
			message, err := io.ReadAll(reader)
			readCancel()
			if err != nil {
				logCtx.Error().Err(err).Msg("failed to read message")
				cancel()
				return
			}

			logCtx.Debug().
				Int("msg_type", int(typ)).
				Int("msg_size", len(message)).
				Msg("received message")

			// Handle text messages as potential agent prompts
			if typ == websocket.MessageText {
				h.handleTextMessage(ctx, message, userID, connWrapper, logCtx)
			} else {
				// Echo non-text messages back
				writeCtx, writeCancel := context.WithTimeout(ctx, writeTimeout)
				if err := conn.Write(writeCtx, typ, message); err != nil {
					writeCancel()
					logCtx.Error().Err(err).Msg("failed to echo message")
					cancel()
					return
				}
				writeCancel()
			}
		}
	}
}

// pingLoop sends periodic ping frames to keep connections alive.
// Uses conn.Ping which handles pong response automatically.
func (h *Handler) pingLoop(ctx context.Context, conn *websocket.Conn, logger zerolog.Logger, cancel context.CancelFunc) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Ping(pingCtx)
			pingCancel()

			if err != nil {
				logger.Debug().Err(err).Msg("ping failed - connection likely dead")
				cancel()
				return
			}
		}
	}
}

// handleTextMessage processes incoming text messages and routes them to the appropriate agent.
// Supported message types: "prompt" (send to agent), "steer" (stop agent), "status" (get agent status).
func (h *Handler) handleTextMessage(ctx context.Context, data []byte, userID string, conn *WSConnWrapper, logger zerolog.Logger) {
	var msg struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		AgentID string `json:"agent_id"` // Required field - which agent to route to
		Text    string `json:"text,omitempty"`
		Content string `json:"content,omitempty"` // for tool calls, etc
	}

	if err := json.Unmarshal(data, &msg); err != nil {
		logger.Debug().Err(err).Msg("failed to unmarshal message")
		return
	}

	switch msg.Type {
	case "prompt":
		if msg.ID == "" {
			logger.Debug().Msg("prompt message missing id field")
			return
		}
		if msg.AgentID == "" {
			logger.Debug().Msg("prompt message missing agent_id field")
			return
		}
		if msg.Text == "" {
			logger.Debug().Msg("prompt message missing text field")
			return
		}

		task := agent.Task{
			Message: msg.Text,
			MsgID:   msg.ID,
		}
		ag := h.agentManager.GetAgent(ctx, userID, msg.AgentID, conn, task)
		if ag == nil {
			logger.Debug().Msg("failed to get agent")
			return
		}
		ag.Submit(task)
	case "steer":
		if msg.AgentID == "" {
			logger.Debug().Msg("steer message missing agent_id field")
			return
		}

		ag := h.agentManager.GetAgent(ctx, userID, msg.AgentID, nil, agent.Task{})
		if ag == nil {
			logger.Debug().Msg("failed to get agent for steer")
			return
		}
		ag.Stop()
	case "status":
		if msg.AgentID == "" {
			logger.Debug().Msg("status message missing agent_id field")
			return
		}

		agentInstance := h.agentManager.GetOrCreateAgentForUser(ctx, userID, msg.AgentID, agent.Task{})
		status := agentInstance.GetStatus()

		statusEvent := wsEvent{
			Type:    "lane_status",
			AgentID: msg.AgentID,
			Data:    status,
		}
		if err := conn.WriteJSON(ctx, statusEvent); err != nil {
			logger.Debug().Err(err).Msg("failed to send status event")
		}
	default:
		logger.Debug().Str("msg_type", msg.Type).Msg("unknown message type")
	}
}

// wsEvent represents an event sent to the WebSocket client as JSON.
type wsEvent struct {
	Type    string `json:"type"`
	AgentID string `json:"agent_id,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// ActiveConns returns the current number of active WebSocket connections.
func (h *Handler) ActiveConns() int {
	return int(atomic.LoadInt32(&h.activeConns))
}
