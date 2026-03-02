package websocket

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSConnWrapper wraps a write channel for agent streaming with thread-safe writes.
//
// # Design Decisions
//
// Why Wrap the Write Channel:
//   - Agents need to write events without knowing WebSocket details
//   - Wrapper provides type-safe WriteJSON method with timeout
//   - Clean separation: agents don't know about wsMessage internals
//   - Thread-safe: mutex protects concurrent agent writes
//
// Write Timeout (100ms):
//   - Fast enough to detect stuck writer goroutine
//   - Slow enough for normal in-memory channel send
//   - Agent continues even if client is slow
//   - Matches connected event timeout in handler
//
// Mutex Protection:
//   - Multiple goroutines may call WriteJSON concurrently
//   - sync.Pool Get/Put and message setup must be atomic with send
//   - Prevents message corruption between goroutines
//   - Low contention: agents typically write sequentially
type WSConnWrapper struct {
	writeCh      chan<- *wsMessage
	mu           sync.Mutex
	writeTimeout time.Duration
}

// NewWSConnWrapper creates a new WebSocket connection wrapper.
func NewWSConnWrapper(writeCh chan<- *wsMessage) *WSConnWrapper {
	return &WSConnWrapper{
		writeCh:      writeCh,
		writeTimeout: 100 * time.Millisecond,
	}
}

// WriteJSON writes JSON data to the WebSocket connection with thread-safety and timeout.
// Returns ErrCloseSent if the write times out (indicating the connection is stuck).
func (w *WSConnWrapper) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	msg := wsMsgPool.Get().(*wsMessage)
	msg.messageType = websocket.TextMessage
	msg.data = data

	select {
	case w.writeCh <- msg:
		return nil
	case <-time.After(w.writeTimeout):
		msg.messageType = 0
		msg.data = nil
		wsMsgPool.Put(msg)
		return websocket.ErrCloseSent
	}
}
