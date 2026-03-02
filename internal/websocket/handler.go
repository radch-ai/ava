// Package websocket provides WebSocket connection handling with multi-agent routing.
package websocket

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/ctx"
	"github.com/rs/zerolog"
)

// Connection configuration constants.
const (
	// defaultBufferSize is the read/write buffer size per connection (8KB).
	defaultBufferSize = 8192

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
	upgrader     websocket.Upgrader
	connSem      chan struct{} // Semaphore for connection limiting
	maxConns     int
	activeConns  int32 // atomic counter
	agentManager *AgentManager
}

// wsMessage represents a message to be written to the WebSocket connection.
type wsMessage struct {
	messageType int
	data        []byte
}

// wsMsgPool is a sync.Pool for wsMessage structs.
// Messages are short-lived and frequently allocated; the pool reuses memory
// across all connections and reduces GC pressure in high-traffic scenarios.
var wsMsgPool = sync.Pool{
	New: func() interface{} {
		return &wsMessage{}
	},
}

// wsEvent represents an event sent to the WebSocket client as JSON.
type wsEvent struct {
	Type    string `json:"type"`
	AgentID string `json:"agent_id,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// NewHandler creates a new WebSocket handler with connection limiting.
func NewHandler(logger zerolog.Logger, allowedOrigins []string, maxConns int, agentOpts *AgentManagerOptions) *Handler {
	upgrader := websocket.Upgrader{
		ReadBufferSize:   defaultBufferSize,
		WriteBufferSize:  defaultBufferSize,
		HandshakeTimeout: 10 * time.Second,
		CheckOrigin: func(r *http.Request) bool {
			return checkOrigin(r, allowedOrigins)
		},
	}

	sem := make(chan struct{}, maxConns)

	return &Handler{
		logger:       logger,
		upgrader:     upgrader,
		connSem:      sem,
		maxConns:     maxConns,
		agentManager: NewAgentManager(logger, agentOpts),
	}
}

// checkOrigin validates the Origin header against allowed origins.
// "*" allows all origins (use with caution in development).
func checkOrigin(r *http.Request, allowedOrigins []string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// No Origin header, allow for same-origin or non-browser clients
		return true
	}

	for _, allowed := range allowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

// HandleWebSocket handles an incoming WebSocket connection upgrade request.
// Each connection spawns three goroutines: Writer, PingLoop, and Main loop.
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
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error().Err(err).Msg("websocket upgrade failed")
		return
	}

	// Ensure connection is closed on all exit paths
	// Note: Don't write close frame here as it violates single-writer pattern
	defer func() { _ = conn.Close() }()

	// Set max message size to prevent memory exhaustion attacks
	conn.SetReadLimit(MaxMessageSize)

	// Generate unique connection ID
	connID := uuid.New().String()

	// Get user_id from context (set by auth middleware)
	userID := ctx.GetUserIDFromContext(r.Context())

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

	// Set initial read deadline and configure pong handler
	if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
		logCtx.Debug().Err(err).Msg("failed to set read deadline")
	}
	conn.SetPongHandler(func(appData string) error {
		// Reset read deadline on pong
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			logCtx.Debug().Err(err).Msg("failed to set read deadline")
		}
		return nil
	})

	// Context for graceful shutdown - cancellable by any goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Watch for client disconnect
	go func() {
		<-r.Context().Done()
		cancel()
	}()

	// Channel for serializing all writes (single writer pattern)
	// Using pointer channel to work with sync.Pool for wsMessage structs
	writeCh := make(chan *wsMessage, 8)
	defer close(writeCh)

	// Channel for writer goroutine to report errors (buffered to prevent blocking)
	writerErrCh := make(chan error, 1)

	// Start single writer goroutine
	go h.writer(ctx, conn, writeCh, logCtx, writerErrCh)

	// Start ping goroutine
	go h.pingLoop(ctx, writeCh, logCtx)

	// Send connected event to client
	connectedEvent := wsEvent{
		Type: "connected",
		Data: map[string]string{
			"user_id": userID,
			"conn_id": connID,
		},
	}
	connectedData, _ := json.Marshal(connectedEvent)
	msg := wsMsgPool.Get().(*wsMessage)
	msg.messageType = websocket.TextMessage
	msg.data = connectedData
	select {
	case writeCh <- msg:
	case <-time.After(100 * time.Millisecond):
		wsMsgPool.Put(msg)
		logCtx.Warn().Msg("failed to send connected event")
	}

	// Message loop
	for {
		select {
		case <-ctx.Done():
			logCtx.Info().Msg("context cancelled, closing connection")
			// Try to send close frame through writer, but don't block
			closeTimer := time.NewTimer(100 * time.Millisecond)
			msg := wsMsgPool.Get().(*wsMessage)
			msg.messageType = websocket.CloseMessage
			msg.data = websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
			select {
			case writeCh <- msg:
				closeTimer.Stop()
				// Close frame queued successfully
			case <-closeTimer.C:
				// Timeout sending close frame, return msg to pool
				msg.messageType = 0
				msg.data = nil
				wsMsgPool.Put(msg)
			}
			return
		case err := <-writerErrCh:
			logCtx.Error().Err(err).Msg("writer failed, closing connection")
			cancel()
			return
		default:
			// Set read deadline before each read
			if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
				logCtx.Debug().Err(err).Msg("failed to set read deadline")
			}

			messageType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					logCtx.Error().Err(err).Msg("unexpected close")
				}
				cancel()
				return
			}

			logCtx.Debug().
				Int("msg_type", messageType).
				Int("msg_size", len(message)).
				Msg("received message")

			// Handle text messages as potential agent prompts
			if messageType == websocket.TextMessage {
				h.handleTextMessage(ctx, message, userID, writeCh, logCtx)
			} else {
				// Echo non-text messages back
				writeTimer := time.NewTimer(writeTimeout)
				msg := wsMsgPool.Get().(*wsMessage)
				msg.messageType = messageType
				msg.data = message
				select {
				case writeCh <- msg:
					writeTimer.Stop()
				case <-ctx.Done():
					writeTimer.Stop()
					msg.messageType = 0
					msg.data = nil
					wsMsgPool.Put(msg)
					return
				case <-writeTimer.C:
					logCtx.Error().Msg("write channel timeout")
					msg.messageType = 0
					msg.data = nil
					wsMsgPool.Put(msg)
					cancel()
					return
				}
			}
		}
	}
}

// writer serializes writes to the WebSocket connection.
// WebSocket requires serialized writes; this goroutine manages deadlines centrally
// and handles ping/pong interleaving. Reports errors via writerErrCh.
func (h *Handler) writer(ctx context.Context, conn *websocket.Conn, writeCh <-chan *wsMessage, logger zerolog.Logger, writerErrCh chan<- error) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-writeCh:
			if !ok {
				return
			}
			if msg.messageType == 0 {
				// Skip invalid messages (already cleaned up)
				wsMsgPool.Put(msg)
				continue
			}
			if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				logger.Debug().Err(err).Msg("failed to set write deadline")
			}
			if err := conn.WriteMessage(msg.messageType, msg.data); err != nil {
				logger.Debug().Err(err).Msg("write failed")
				// Return to pool and continue
				msg.messageType = 0
				msg.data = nil
				wsMsgPool.Put(msg)
				// Use select to avoid blocking if context is cancelled
				select {
				case writerErrCh <- err:
				case <-ctx.Done():
				}
				return
			}
			// Reset and return to pool for reuse
			msg.messageType = 0
			msg.data = nil
			wsMsgPool.Put(msg)
		}
	}
}

// pingLoop sends periodic ping frames to keep connections alive.
func (h *Handler) pingLoop(ctx context.Context, writeCh chan<- *wsMessage, logger zerolog.Logger) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Use a timer to avoid blocking forever on write
			pingTimer := time.NewTimer(writeTimeout)
			msg := wsMsgPool.Get().(*wsMessage)
			msg.messageType = websocket.PingMessage
			msg.data = nil
			select {
			case writeCh <- msg:
				pingTimer.Stop()
			case <-ctx.Done():
				pingTimer.Stop()
				msg.messageType = 0
				msg.data = nil
				wsMsgPool.Put(msg)
				return
			case <-pingTimer.C:
				pingTimer.Stop()
				msg.messageType = 0
				msg.data = nil
				wsMsgPool.Put(msg)
				logger.Debug().Msg("ping send timeout - connection likely dead")
				return
			}
		}
	}
}

// handleTextMessage processes incoming text messages and routes them to the appropriate agent.
// Supported message types: "prompt" (send to agent), "steer" (stop agent), "status" (get agent status).
func (h *Handler) handleTextMessage(ctx context.Context, data []byte, userID string, writeCh chan<- *wsMessage, logger zerolog.Logger) {
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
		connWrapper := NewWSConnWrapper(writeCh)
		ag := h.agentManager.GetAgent(ctx, userID, msg.AgentID, connWrapper, task)
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
		statusData, _ := json.Marshal(statusEvent)
		respMsg := wsMsgPool.Get().(*wsMessage)
		respMsg.messageType = websocket.TextMessage
		respMsg.data = statusData
		select {
		case writeCh <- respMsg:
		case <-time.After(100 * time.Millisecond):
			wsMsgPool.Put(respMsg)
			logger.Debug().Msg("failed to send status event")
		}
	default:
		logger.Debug().Str("msg_type", msg.Type).Msg("unknown message type")
	}
}

// ActiveConns returns the current number of active WebSocket connections.
func (h *Handler) ActiveConns() int {
	return int(atomic.LoadInt32(&h.activeConns))
}
