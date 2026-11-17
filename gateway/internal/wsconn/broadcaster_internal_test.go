package wsconn

// Capacity-sensitive pins for the per-connection outbound queues.
// Internal so they can construct a broadcaster with a tiny queue and
// reach the registered outbounds directly.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + url[4:]
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	return c
}

// acceptAndStall serves one websocket that never reads; its handler
// exits when either side closes the socket, signaling on the returned
// channel. The exit signal is a blocked read, not the request context
// — hijacked connections never cancel it.
func acceptAndStall(t *testing.T) (*httptest.Server, <-chan struct{}) {
	t.Helper()
	closed := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer close(closed)
		defer c.CloseNow()
		_, _, _ = c.Read(context.Background())
	}))
	t.Cleanup(s.Close)
	return s, closed
}

// A connection whose queue overflows is dropped by the enqueue
// itself: with capacity 2, a queue nobody drains must reject its third
// publish, no call ever blocks, and the eviction closes the peer's
// socket — a dropped-but-open connection is a zombie that never
// reconnects. The outbound is built without a writer so the fill is
// deterministic.
func TestWriteJSONOverflowDropsConn(t *testing.T) {
	b := newBroadcaster(zerolog.Nop(), time.Second, 2)
	s, serverClosed := acceptAndStall(t)
	c := dial(t, s.URL)
	o := &outbound{
		conn: c,
		out:  make(chan []byte, 2),
		stop: make(chan struct{}),
	}
	b.conns["stuck"] = o

	ev := map[string]string{"type": "test"}
	start := time.Now()
	require.NoError(t, b.WriteJSON(context.Background(), ev))
	require.NoError(t, b.WriteJSON(context.Background(), ev))
	require.NoError(t, b.WriteJSON(context.Background(), ev))
	require.Less(t, time.Since(start), 500*time.Millisecond,
		"enqueue must never block on a full peer queue")

	require.Equal(t, 0, b.Count(), "overflowed connection must be dropped")
	select {
	case <-o.stop:
	default:
		t.Fatal("overflowed outbound must be shut down")
	}
	select {
	case <-serverClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("overflow eviction must close the peer's socket so it can reconnect")
	}
}

// Remove stops the connection's writer goroutine: it signals exit and
// no later publish touches the removed connection.
func TestRemoveStopsWriter(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow()
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	}))
	t.Cleanup(s.Close)
	c := dial(t, s.URL)
	defer c.CloseNow()

	b := newBroadcaster(zerolog.Nop(), time.Second, 8)
	b.Add("gone", c)

	b.mu.Lock()
	o := b.conns["gone"]
	b.mu.Unlock()
	require.NotNil(t, o)

	b.Remove("gone")

	// The writer's exit is requested through its stop channel; the
	// loop selects on it between writes.
	select {
	case <-o.stop:
	default:
		t.Fatal("Remove must stop the connection's writer")
	}

	require.NoError(t, b.WriteJSON(context.Background(), map[string]string{"type": "after"}))
	require.Equal(t, 0, b.Count())
}

// A join's backlog rides the joining connection's own queue: the map
// lock is never held across a socket write. With the synchronous
// backlog write, a joiner whose socket cannot drain (write timeout
// 10s) parks the lock for ~10s and every publisher waits behind it.
func TestAddWithBacklogDoesNotWriteUnderMapLock(t *testing.T) {
	b := newBroadcaster(zerolog.Nop(), 10*time.Second, 64)
	s, _ := acceptAndStall(t)
	c := dial(t, s.URL)

	go func() {
		b.AddWithBacklog("joiner", c, func() []any {
			return []any{
				map[string]string{"type": "queue_state"},
				map[string]string{"type": "message_chunk", "data": strings.Repeat("x", 8<<20)},
			}
		})
	}()

	// Detect the join holding the map lock: with a synchronous
	// backlog write it stays held for the write-timeout duration
	// (10s here); with a queued backlog the critical section is an
	// enqueue and the spin times out into the fast path. The timed
	// publish below then measures exactly the producer's exposure to
	// the join's critical section.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !b.mu.TryLock() {
			break
		}
		b.mu.Unlock()
		time.Sleep(time.Millisecond)
	}

	start := time.Now()
	require.NoError(t, b.WriteJSON(context.Background(), map[string]string{"type": "tick"}))
	require.Less(t, time.Since(start), 2*time.Second,
		"publish must not wait behind a slow joiner's backlog write")
}
