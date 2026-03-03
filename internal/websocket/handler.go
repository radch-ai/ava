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
	"github.com/nevaria-ai/gateway/internal/wsconn"
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
	sem := make(chan struct{}, maxConns)
	return &Handler{
		logger: logger,
		acceptOpts: websocket.AcceptOptions{
			OriginPatterns: allowedOrigins,
		},
		connSem:      sem,
		maxConns:     maxConns,
		agentManager: NewAgentManager(logger, agentOpts),
	}
}

// HandleWebSocket handles an incoming WebSocket connection upgrade request.
// Each connection is dedicated to a single agent lane — no multiplexing.
// One goroutine is spawned for the ping loop; reads happen on the main goroutine.
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

	logger := h.logger.With().
		Str("conn_id", connID).
		Str("user_id", userID).
		Str("client_ip", clientIP).
		Int("active_conns", int(atomic.LoadInt32(&h.activeConns))).
		Logger()

	logger.Info().Msg("websocket connection established")

	// Context for graceful shutdown - cancellable by any goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Watch for client disconnect
	go func() {
		<-r.Context().Done()
		cancel()
	}()

	// Start ping goroutine to keep connection alive
	go h.pingLoop(ctx, conn, logger, cancel)

	// Send connected event to client
	writeCtx, writeCancel := context.WithTimeout(ctx, writeTimeout)
	err = wsjson.Write(writeCtx, conn, wsEvent{
		Type: "connected",
		Data: map[string]string{"user_id": userID, "conn_id": connID},
	})
	writeCancel()
	if err != nil {
		logger.Warn().Err(err).Msg("failed to send connected event")
		return
	}

	// One wsconn.Wrapper per connection — this conn is dedicated to one agent lane.
	// On reconnect, UpdateConn swaps the underlying conn atomically for agent streaming
	connWrapper := wsconn.NewWrapper(conn)

	// Message loop
	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("context cancelled, closing connection")
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		default:
			readCtx, readCancel := context.WithTimeout(ctx, readTimeout)
			typ, reader, err := conn.Reader(readCtx)
			if err != nil {
				readCancel()
				if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
					websocket.CloseStatus(err) == websocket.StatusGoingAway {
					logger.Info().Msg("client closed connection")
				} else {
					logger.Error().Err(err).Msg("read error")
				}
				cancel()
				return
			}

			message, err := io.ReadAll(reader)
			readCancel()
			if err != nil {
				logger.Error().Err(err).Msg("failed to read message")
				cancel()
				return
			}

			logger.Debug().Int("msg_type", int(typ)).Int("msg_size", len(message)).Msg("received message")

			// Handle text messages as potential agent prompts
			if typ == websocket.MessageText {
				h.handleTextMessage(ctx, message, userID, connWrapper, logger)
			} else {
				// Echo non-text messages back
				writeCtx, writeCancel := context.WithTimeout(ctx, writeTimeout)
				if err := conn.Write(writeCtx, typ, message); err != nil {
					writeCancel()
					logger.Error().Err(err).Msg("failed to echo message")
					cancel()
					return
				}
				writeCancel()
			}
		}
	}
}

// pingLoop sends periodic ping frames to keep the connection alive.
// coder/websocket handles pong responses automatically.
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

// incomingMsg is the expected shape of all client messages.
type incomingMsg struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	AgentID string `json:"agent_id"`
	Content string `json:"content,omitempty"`
}

// handleTextMessage parses and routes incoming client messages to the appropriate agent.
//
// Supported message types:
//   - "prompt": submit a task to the agent, creating it if needed
//   - "stop":   stop the agent's current task
//   - "status": return the agent's current status over the connection
//
// On reconnect, "prompt" also refreshes the agent's conn via UpdateConn so
// subsequent streaming events go to the new connection.
func (h *Handler) handleTextMessage(ctx context.Context, data []byte, userID string, conn *wsconn.Wrapper, logger zerolog.Logger) {
	var msg incomingMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.Debug().Err(err).Msg("failed to unmarshal message")
		return
	}

	if msg.AgentID == "" {
		logger.Debug().Msg("missing agent_id")
		return
	}

	switch msg.Type {
	case "prompt":
		if msg.ID == "" || msg.Content == "" {
			logger.Debug().Msg("prompt missing id or text")
			return
		}
		h.agentManager.SubmitMsg(ctx, userID, msg.AgentID, conn, agent.Message{Content: msg.Content, ID: msg.ID})

	case "interrupt":
		ag := h.agentManager.Get(userID, msg.AgentID)
		if ag != nil {
			ag.Interrupt()
		}

	case "status":
		ag := h.agentManager.Get(userID, msg.AgentID)
		if ag != nil {
			if err := conn.WriteJSON(ctx, wsEvent{
				Type:    "lane_status",
				AgentID: msg.AgentID,
				Data:    ag.GetStatus(),
			}); err != nil {
				logger.Debug().Err(err).Msg("failed to send status event")
			}
		}

	default:
		logger.Debug().Str("msg_type", msg.Type).Msg("unknown message type")
	}
}

// wsEvent is the JSON envelope for all server-to-client events.
type wsEvent struct {
	Type    string `json:"type"`
	AgentID string `json:"agent_id,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// ActiveConns returns the current number of active WebSocket connections.
func (h *Handler) ActiveConns() int {
	return int(atomic.LoadInt32(&h.activeConns))
}
