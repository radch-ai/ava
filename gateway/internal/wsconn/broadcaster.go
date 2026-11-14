// Package wsconn provides WebSocket connection management types.
package wsconn

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/rs/zerolog"
)

// DefaultWriteTimeout is the default timeout for writes.
const DefaultWriteTimeout = 10 * time.Second

// Broadcaster manages multiple WebSocket connections for fan-out streaming.
// Thread-safe: uses RWMutex for concurrent access to connection map.
type Broadcaster struct {
	mu           sync.RWMutex
	conns        map[string]*websocket.Conn // connID -> connection
	writeTimeout time.Duration
	logger       zerolog.Logger
}

// NewBroadcaster creates a broadcaster with no connections and the
// default write timeout.
func NewBroadcaster(logger zerolog.Logger) *Broadcaster {
	return NewBroadcasterWithTimeout(logger, DefaultWriteTimeout)
}

// NewBroadcasterWithTimeout creates a broadcaster whose per-conn
// writes are bounded by the given deadline; non-positive falls back
// to the default.
func NewBroadcasterWithTimeout(logger zerolog.Logger, writeTimeout time.Duration) *Broadcaster {
	if writeTimeout <= 0 {
		writeTimeout = DefaultWriteTimeout
	}
	return &Broadcaster{
		conns:        make(map[string]*websocket.Conn),
		writeTimeout: writeTimeout,
		logger:       logger,
	}
}

// Add adds a connection to the broadcast list.
// If connID already exists, the old connection is replaced.
// Nil connections are ignored.
func (b *Broadcaster) Add(connID string, conn *websocket.Conn) {
	if conn == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Close old connection if replacing
	if old, exists := b.conns[connID]; exists && old != conn {
		_ = old.Close(websocket.StatusNormalClosure, "replaced by new connection")
	}

	b.conns[connID] = conn
	b.logger.Debug().Str("conn_id", connID).Int("total", len(b.conns)).Msg("connection added to broadcaster")
}

// Remove removes a connection from the broadcast list.
// Safe to call even if connID doesn't exist.
func (b *Broadcaster) Remove(connID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	delete(b.conns, connID)
	b.logger.Debug().Str("conn_id", connID).Int("total", len(b.conns)).Msg("connection removed from broadcaster")
}

// Count returns the number of active connections.
func (b *Broadcaster) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.conns)
}

// WriteJSON writes v as JSON to all connections, each write running
// concurrently under its own deadline — a stalled peer is dropped
// when its deadline passes, never waited on by the others. The map
// lock stays held for the whole fan-out so an Add cannot interleave
// its first event between peers' writes. Removes any connections
// that fail to write.
func (b *Broadcaster) WriteJSON(ctx context.Context, v interface{}) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.conns) == 0 {
		return nil // No connections, nothing to do
	}

	var failedMu sync.Mutex
	var failed []string
	var wg sync.WaitGroup
	for connID, conn := range b.conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			writeCtx, cancel := context.WithTimeout(ctx, b.writeTimeout)
			defer cancel()
			if err := wsjson.Write(writeCtx, conn, v); err != nil {
				b.logger.Debug().Err(err).Str("conn_id", connID).
					Msg("failed to write to connection, removing")
				failedMu.Lock()
				failed = append(failed, connID)
				failedMu.Unlock()
			}
		}()
	}
	wg.Wait()

	// Remove failed connections.
	for _, connID := range failed {
		delete(b.conns, connID)
	}

	return nil
}

// HasConnections returns true if there are any active connections.
func (b *Broadcaster) HasConnections() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.conns) > 0
}

// ConnIDs returns a list of all connection IDs (for debugging).
func (b *Broadcaster) ConnIDs() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	ids := make([]string, 0, len(b.conns))
	for id := range b.conns {
		ids = append(ids, id)
	}
	return ids
}
