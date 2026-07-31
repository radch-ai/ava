// Package wsconn provides WebSocket connection wrapper types.
package wsconn

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// DefaultWriteTimeout is the default timeout for writes.
const DefaultWriteTimeout = 10 * time.Second

// Wrapper wraps a websocket.Conn for agent streaming.
//
// # Design
// One wrapper per agent — each agent lane has its own WebSocket connection.
// This avoids multiplexing complexity; agents stream independently.
//
// # Thread Safety
// Uses atomic.Pointer for the conn so UpdateConn can swap the connection
// on reconnect without a mutex. coder/websocket handles concurrent writes
// internally so no additional locking is needed for writes.
//
// # Write Timeout
// Context with timeout prevents blocking on slow clients.
// Agent continues processing even if client is temporarily slow.
type Wrapper struct {
	conn         atomic.Pointer[websocket.Conn]
	writeTimeout time.Duration
}

// NewWrapper creates a new wrapper for the given connection.
func NewWrapper(conn *websocket.Conn) *Wrapper {
	w := &Wrapper{
		writeTimeout: DefaultWriteTimeout,
	}
	w.conn.Store(conn)
	return w
}

// UpdateConn swaps the underlying connection on client reconnect.
// Any in-flight writes using the old conn will error out naturally.
// New writes after this call transparently use the new conn.
func (w *Wrapper) UpdateConn(conn *websocket.Conn) {
	w.conn.Store(conn)
}

// Conn returns the current underlying websocket connection.
func (w *Wrapper) Conn() *websocket.Conn {
	return w.conn.Load()
}

// WriteJSON writes v as JSON to the connection with a write timeout.
// Returns an error if the write fails or times out.
func (w *Wrapper) WriteJSON(ctx context.Context, v interface{}) error {
	writeCtx, cancel := context.WithTimeout(ctx, w.writeTimeout)
	defer cancel()
	return wsjson.Write(writeCtx, w.conn.Load(), v)
}
