// Package wsconn provides WebSocket connection management types.
package wsconn

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
)

// DefaultWriteTimeout is the default timeout for writes.
const DefaultWriteTimeout = 10 * time.Second

// outboundQueueCapacity bounds one connection's pending events. A peer
// this far behind is dead: it is dropped rather than allowed to stall
// producers. Full-replacement snapshots make the drop lossless for the
// peer — a reconnecting client resyncs from the next snapshot.
const outboundQueueCapacity = 256

// outbound is one connection's event queue with its dedicated writer.
// out is never closed — producers enqueue non-blocking and a stopped
// writer's leftovers are garbage once the outbound is unregistered —
// so no enqueue can panic against a concurrent shutdown. Evictions
// close the socket: a connection the broadcaster gives up on must be
// forced to reconnect, not left open and silent.
type outbound struct {
	conn     *websocket.Conn
	out      chan []byte
	stop     chan struct{}
	stopOnce sync.Once
}

// shutdown asks the writer to exit; safe from any goroutine.
func (o *outbound) shutdown() {
	o.stopOnce.Do(func() { close(o.stop) })
}

// Broadcaster manages multiple WebSocket connections for fan-out
// streaming. Publishing is a non-blocking enqueue under the map lock;
// each connection's own writer goroutine drains its queue, so no
// producer ever waits on a peer's socket.
type Broadcaster struct {
	mu           sync.Mutex
	conns        map[string]*outbound // connID -> outbound
	writeTimeout time.Duration
	capacity     int
	logger       zerolog.Logger
}

// NewBroadcaster creates a broadcaster with no connections and the
// default write timeout.
func NewBroadcaster(logger zerolog.Logger) *Broadcaster {
	return NewBroadcasterWithTimeout(logger, DefaultWriteTimeout)
}

// NewBroadcasterWithTimeout creates a broadcaster whose per-conn
// writes are bounded by the given deadline; non-positive falls back to
// the default.
func NewBroadcasterWithTimeout(logger zerolog.Logger, writeTimeout time.Duration) *Broadcaster {
	return newBroadcaster(logger, writeTimeout, outboundQueueCapacity)
}

// newBroadcaster builds a broadcaster with an explicit per-conn queue
// capacity; the unexported seam exists for capacity-sensitive tests.
func newBroadcaster(logger zerolog.Logger, writeTimeout time.Duration, capacity int) *Broadcaster {
	if writeTimeout <= 0 {
		writeTimeout = DefaultWriteTimeout
	}
	return &Broadcaster{
		conns:        make(map[string]*outbound),
		writeTimeout: writeTimeout,
		capacity:     capacity,
		logger:       logger,
	}
}

// registerLocked installs a connection's outbound and starts its
// writer; caller holds mu.
func (b *Broadcaster) registerLocked(connID string, conn *websocket.Conn) *outbound {
	o := &outbound{
		conn: conn,
		out:  make(chan []byte, b.capacity),
		stop: make(chan struct{}),
	}
	b.conns[connID] = o
	go b.writeLoop(connID, o)
	return o
}

// replaceLocked shuts down a same-id predecessor; caller holds mu.
func (b *Broadcaster) replaceLocked(connID string, conn *websocket.Conn) {
	if old, exists := b.conns[connID]; exists && old.conn != conn {
		_ = old.conn.Close(websocket.StatusNormalClosure, "replaced by new connection")
		old.shutdown()
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

	b.replaceLocked(connID, conn)
	b.registerLocked(connID, conn)
	b.logger.Debug().Str("conn_id", connID).Int("total", len(b.conns)).Msg("connection added to broadcaster")
}

// Remove removes a connection from the broadcast list and stops its
// writer. Safe to call even if connID does not exist.
func (b *Broadcaster) Remove(connID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if o, ok := b.conns[connID]; ok {
		delete(b.conns, connID)
		o.shutdown()
	}
	b.logger.Debug().Str("conn_id", connID).Int("total", len(b.conns)).Msg("connection removed from broadcaster")
}

// WriteJSON marshals v once and enqueues it on every connection — a
// non-blocking send under the map lock, so publishers serialize only
// on the enqueue and never on a peer's socket. Per-connection FIFO is
// the ordering contract: the order of WriteJSON calls is the order
// each connection reads its frames. A full queue or a failing write
// evicts the connection — evictions close the socket, so the dropped
// peer reconnects and resyncs from the next full-replacement snapshot
// instead of lingering as a silent zombie. The context is unused —
// writes are bounded by the per-conn deadline, not any producer's.
func (b *Broadcaster) WriteJSON(_ context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for connID, o := range b.conns {
		select {
		case o.out <- data:
		default:
			b.evictLocked(connID, o)
			b.logger.Debug().Str("conn_id", connID).
				Msg("outbound queue overflow, dropping connection")
		}
	}
	return nil
}

// evictLocked unregisters a connection, stops its writer, and closes
// its socket. Caller holds mu.
func (b *Broadcaster) evictLocked(connID string, o *outbound) {
	if cur, ok := b.conns[connID]; ok && cur == o {
		delete(b.conns, connID)
	}
	o.shutdown()
	_ = o.conn.CloseNow()
}

// writeLoop drains one connection's queue to its socket. A write
// error ends the loop and evicts the connection — closing the socket,
// since a peer that cannot be written to must reconnect rather than
// keep an open-but-silent connection. Shutdown ends it without
// touching the socket — the owner closes removed connections.
func (b *Broadcaster) writeLoop(connID string, o *outbound) {
	for {
		select {
		case data := <-o.out:
			ctx, cancel := context.WithTimeout(context.Background(), b.writeTimeout)
			err := o.conn.Write(ctx, websocket.MessageText, data)
			cancel()
			if err == nil {
				continue
			}
			b.logger.Debug().Err(err).Str("conn_id", connID).
				Msg("failed to write to connection, removing")
			b.mu.Lock()
			b.evictLocked(connID, o)
			b.mu.Unlock()
			return
		case <-o.stop:
			return
		}
	}
}

// AddWithBacklog adds a connection and first delivers the backlog the
// callback builds. The callback, the registration, and the backlog's
// enqueue are one critical section against concurrent WriteJSON
// publishes, so the backlog a joiner receives is never missing an
// event that postdates its snapshot — per-connection FIFO then keeps
// the backlog ahead of everything enqueued after it. No socket write
// happens under the lock: the frames ride the connection's own queue
// and its writer drains them. A backlog that fails to marshal or fit
// the queue drops the join. The callback runs with the lock held;
// build the backlog from lane state under the lane's own mutex there.
func (b *Broadcaster) AddWithBacklog(connID string, conn *websocket.Conn, backlog func() []any) {
	if conn == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.replaceLocked(connID, conn)
	o := b.registerLocked(connID, conn)
	for _, v := range backlog() {
		data, err := json.Marshal(v)
		if err != nil {
			b.logger.Debug().Err(err).Str("conn_id", connID).
				Msg("backlog marshal failed, dropping join")
			b.evictLocked(connID, o)
			return
		}
		select {
		case o.out <- data:
		default:
			b.logger.Debug().Str("conn_id", connID).
				Msg("backlog overflowed queue, dropping join")
			b.evictLocked(connID, o)
			return
		}
	}
	b.logger.Debug().Str("conn_id", connID).Int("total", len(b.conns)).
		Msg("connection added to broadcaster")
}

// Count returns the number of active connections.
func (b *Broadcaster) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.conns)
}
