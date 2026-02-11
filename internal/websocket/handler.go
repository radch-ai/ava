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
	"github.com/rs/zerolog"
)

const (
	defaultBufferSize = 8192
	maxMessageSize    = 65536 // 64KB
	readTimeout       = 60 * time.Second
	writeTimeout      = 10 * time.Second
	pingInterval      = 30 * time.Second
)

var upgrader = websocket.Upgrader{
	// 8KB buffers balance memory usage with throughput for production scale.
	// Each connection allocates 16KB total (8KB read + 8KB write).
	// With 100K concurrent connections: ~1.6GB RAM for buffers alone.
	// Horizontal scaling required for viral growth.
	ReadBufferSize:   defaultBufferSize,
	WriteBufferSize:  defaultBufferSize,
	HandshakeTimeout: 10 * time.Second,
}

// A Handler manages WebSocket connections with connection limiting and origin validation.
type Handler struct {
	logger       zerolog.Logger
	upgrader     websocket.Upgrader
	connSem      chan struct{} // Semaphore for connection limiting
	maxConns     int
	activeConns  int32 // atomic counter
	agentManager *AgentManager
}

type wsMessage struct {
	messageType int
	data        []byte
}

// wsMsgPool is a sync.Pool for wsMessage structs.
var wsMsgPool = sync.Pool{
	New: func() interface{} {
		return &wsMessage{}
	},
}

// wsEvent represents an event sent to the WebSocket client as JSON.
type wsEvent struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// marshalEvent converts an event to JSON bytes for WebSocket transmission.
func marshalEvent(event wsEvent) []byte {
	data, _ := json.Marshal(event)
	return data
}

// NewHandler creates a new WebSocket handler with connection limiting.
func NewHandler(logger zerolog.Logger, allowedOrigins []string, maxConns int) *Handler {

	// Create upgrader with origin checking
	handlerUpgrader := upgrader // Copy package default
	handlerUpgrader.CheckOrigin = func(r *http.Request) bool {
		return checkOrigin(r, allowedOrigins)
	}

	// Create semaphore if maxConns > 0
	var sem chan struct{}
	if maxConns > 0 {
		sem = make(chan struct{}, maxConns)
	}

	return &Handler{
		logger:       logger,
		upgrader:     handlerUpgrader,
		connSem:      sem,
		maxConns:     maxConns,
		agentManager: NewAgentManager(logger),
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
	conn.SetReadLimit(maxMessageSize)

	// Generate unique connection ID and session ID
	connID := uuid.New().String()
	sessionID := uuid.New().String()
	logCtx := h.logger.With().
		Str("conn_id", connID).
		Str("remote_addr", r.RemoteAddr).
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

	// Create or get agent for this session
	agentInstance := h.agentManager.GetOrCreateAgent(sessionID)
	agentState := agentInstance.State()
	agentID := agentState.SessionID // Use session ID as agent ID for now

	// Subscribe to agent events
	subID, eventCh := agentInstance.Subscribe()
	defer agentInstance.Unsubscribe(subID)

	// Start goroutine to forward agent events to WebSocket
	go h.forwardAgentEvents(ctx, eventCh, writeCh, logCtx)

	// Send connected event to client
	connectedEvent := wsEvent{
		Type: "connected",
		Data: map[string]string{
			"session_id": sessionID,
			"agent_id":   agentID,
			"conn_id":    connID,
		},
	}
	msg := wsMsgPool.Get().(*wsMessage)
	msg.messageType = websocket.TextMessage
	msg.data = marshalEvent(connectedEvent)
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
			// Clean up agent on disconnect
			h.agentManager.RemoveAgent(sessionID)
			return
		case err := <-writerErrCh:
			logCtx.Error().Err(err).Msg("writer failed, closing connection")
			cancel()
			// Clean up agent on disconnect
			h.agentManager.RemoveAgent(sessionID)
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
				// Clean up agent on disconnect
				h.agentManager.RemoveAgent(sessionID)
				return
			}

			logCtx.Debug().
				Str("msg_type", messageTypeToString(messageType)).
				Int("msg_size", len(message)).
				Msg("received message")

			// Handle text messages as potential agent prompts
			if messageType == websocket.TextMessage {
				h.handleTextMessage(ctx, message, agentInstance, writeCh, logCtx)
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
					// Return msg to pool on context cancellation
					msg.messageType = 0
					msg.data = nil
					wsMsgPool.Put(msg)
					// Clean up agent on disconnect
					h.agentManager.RemoveAgent(sessionID)
					return
				case <-writeTimer.C:
					logCtx.Error().Msg("write channel timeout")
					// Return msg to pool on timeout
					msg.messageType = 0
					msg.data = nil
					wsMsgPool.Put(msg)
					cancel()
					// Clean up agent on disconnect
					h.agentManager.RemoveAgent(sessionID)
					return
				}
			}
		}
	}
}

// writer serializes writes to the WebSocket connection.
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
				// Return to pool on context cancellation
				msg.messageType = 0
				wsMsgPool.Put(msg)
				return
			case <-pingTimer.C:
				pingTimer.Stop()
				// Return to pool on timeout
				msg.messageType = 0
				wsMsgPool.Put(msg)
				logger.Debug().Msg("ping send timeout - connection likely dead")
				return
			}
		}
	}
}

// forwardAgentEvents forwards agent events to the WebSocket client.
func (h *Handler) forwardAgentEvents(ctx context.Context, eventCh <-chan agent.AgentEvent, writeCh chan<- *wsMessage, logger zerolog.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			wsEvt := h.agentEventToWSEvent(event)
			if wsEvt == nil {
				continue
			}
			msg := wsMsgPool.Get().(*wsMessage)
			msg.messageType = websocket.TextMessage
			msg.data = marshalEvent(*wsEvt)

			// Try to send with timeout, using a select that catches channel close
			sendTimer := time.NewTimer(100 * time.Millisecond)
			select {
			case writeCh <- msg:
				sendTimer.Stop()
				// Writer will handle pool return
			case <-ctx.Done():
				sendTimer.Stop()
				// Context cancelled - return msg to pool and exit
				msg.messageType = 0
				msg.data = nil
				wsMsgPool.Put(msg)
				return
			case <-sendTimer.C:
				sendTimer.Stop()
				// Timeout, skip this event
				msg.messageType = 0
				msg.data = nil
				wsMsgPool.Put(msg)
				logger.Debug().Msg("agent event forward timeout")
			}
		}
	}
}

// handleTextMessage handles incoming text messages from the WebSocket client.
func (h *Handler) handleTextMessage(ctx context.Context, data []byte, agentInstance *agent.Agent, writeCh chan<- *wsMessage, logger zerolog.Logger) {
	var msg struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}

	if err := json.Unmarshal(data, &msg); err != nil {
		logger.Debug().Err(err).Msg("failed to unmarshal message")
		return
	}

	switch msg.Type {
	case "prompt":
		if msg.Text == "" {
			logger.Debug().Msg("prompt message missing text field")
			return
		}
		// Send prompt to agent
		if err := agentInstance.Prompt(ctx, msg.Text); err != nil {
			logger.Error().Err(err).Msg("agent prompt failed")
		}
	default:
		logger.Debug().Str("msg_type", msg.Type).Msg("unknown message type")
	}
}

// agentEventToWSEvent converts an agent event to a WebSocket event.
func (h *Handler) agentEventToWSEvent(event agent.AgentEvent) *wsEvent {
	switch e := event.(type) {
	case agent.AgentStartEvent:
		return &wsEvent{
			Type: "agent_start",
			Data: map[string]any{
				"timestamp": e.Timestamp,
			},
		}
	case agent.AgentEndEvent:
		return &wsEvent{
			Type: "agent_end",
			Data: map[string]any{
				"timestamp": e.Timestamp,
				"messages":  e.Messages,
			},
		}
	case agent.TurnStartEvent:
		return &wsEvent{
			Type: "turn_start",
			Data: map[string]any{
				"timestamp": e.Timestamp,
			},
		}
	case agent.TurnEndEvent:
		return &wsEvent{
			Type: "turn_end",
			Data: map[string]any{
				"timestamp":   e.Timestamp,
				"message":     e.Message,
				"toolResults": e.ToolResults,
			},
		}
	case agent.MessageStartEvent:
		return &wsEvent{
			Type: "message_start",
			Data: map[string]any{
				"timestamp": e.Timestamp,
				"message":   e.Message,
			},
		}
	case agent.MessageUpdateEvent:
		return &wsEvent{
			Type: "message_update",
			Data: map[string]any{
				"timestamp": e.Timestamp,
				"message":   e.Message,
				"delta":     e.Delta,
			},
		}
	case agent.MessageEndEvent:
		return &wsEvent{
			Type: "message_end",
			Data: map[string]any{
				"timestamp": e.Timestamp,
				"message":   e.Message,
			},
		}
	case agent.ToolExecutionStartEvent:
		return &wsEvent{
			Type: "tool_start",
			Data: map[string]any{
				"timestamp":    e.Timestamp,
				"tool_call_id": e.ToolCallID,
				"tool_name":    e.ToolName,
				"args":         e.Args,
			},
		}
	case agent.ToolExecutionUpdateEvent:
		return &wsEvent{
			Type: "tool_update",
			Data: map[string]any{
				"timestamp":      e.Timestamp,
				"tool_call_id":   e.ToolCallID,
				"tool_name":      e.ToolName,
				"partial_result": e.PartialResult,
			},
		}
	case agent.ToolExecutionEndEvent:
		return &wsEvent{
			Type: "tool_end",
			Data: map[string]any{
				"timestamp":    e.Timestamp,
				"tool_call_id": e.ToolCallID,
				"tool_name":    e.ToolName,
				"result":       e.Result,
				"is_error":     e.IsError,
			},
		}
	default:
		return nil
	}
}

func messageTypeToString(t int) string {
	switch t {
	case websocket.TextMessage:
		return "text"
	case websocket.BinaryMessage:
		return "binary"
	case websocket.CloseMessage:
		return "close"
	case websocket.PingMessage:
		return "ping"
	case websocket.PongMessage:
		return "pong"
	default:
		return "unknown"
	}
}

// ActiveConns returns the current number of active WebSocket connections.
func (h *Handler) ActiveConns() int {
	return int(atomic.LoadInt32(&h.activeConns))
}
