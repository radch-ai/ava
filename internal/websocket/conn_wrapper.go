package websocket

import (
	"context"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// WSConnWrapper wraps a websocket.Conn for agent streaming.
//
// # Design Decisions
//
// Why Wrap the Connection:
//   - Agents need to write events without knowing WebSocket details
//   - Wrapper provides type-safe WriteJSON method with timeout
//   - Clean separation: agents don't know about WebSocket internals
//
// No Mutex Needed:
//   - coder/websocket handles concurrent writes safely
//   - Library manages write serialization internally
//   - Simpler than gorilla/websocket which required single-writer pattern
//
// Write Timeout:
//   - Context with timeout prevents blocking on slow clients
//   - Agent continues even if client is slow
//   - Timeout error indicates connection issues
type WSConnWrapper struct {
	conn         *websocket.Conn
	writeTimeout time.Duration
}

// NewWSConnWrapper creates a new WebSocket connection wrapper.
func NewWSConnWrapper(conn *websocket.Conn) *WSConnWrapper {
	return &WSConnWrapper{
		conn:         conn,
		writeTimeout: writeTimeout,
	}
}

// WriteJSON writes JSON data to the WebSocket connection with timeout.
// Returns error if the write fails or times out.
func (w *WSConnWrapper) WriteJSON(ctx context.Context, v interface{}) error {
	writeCtx, cancel := context.WithTimeout(ctx, w.writeTimeout)
	defer cancel()
	return wsjson.Write(writeCtx, w.conn, v)
}
