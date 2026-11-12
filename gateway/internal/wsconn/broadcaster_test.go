package wsconn_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/gateway/internal/wsconn"
)

func TestBroadcaster_New(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	assert.NotNil(t, b)
	assert.Equal(t, 0, b.Count())
	assert.False(t, b.HasConnections())
}

func TestBroadcaster_AddNil(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	// Adding nil should be ignored
	b.Add("conn-1", nil)

	assert.Equal(t, 0, b.Count())
	assert.False(t, b.HasConnections())
}

func TestBroadcaster_AddAndCount(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	// Create mock connections
	s1 := newMockWSserver(t)
	defer s1.Close()
	c1 := connectWS(t, s1.URL)
	defer closeConn(c1)

	s2 := newMockWSserver(t)
	defer s2.Close()
	c2 := connectWS(t, s2.URL)
	defer closeConn(c2)

	// Add connections
	b.Add("conn-1", c1)
	assert.Equal(t, 1, b.Count())

	b.Add("conn-2", c2)
	assert.Equal(t, 2, b.Count())

	assert.True(t, b.HasConnections())
	assert.ElementsMatch(t, []string{"conn-1", "conn-2"}, b.ConnIDs())
}

func TestBroadcaster_AddReplace(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	s1 := newMockWSserver(t)
	defer s1.Close()
	c1 := connectWS(t, s1.URL)

	s2 := newMockWSserver(t)
	defer s2.Close()
	c2 := connectWS(t, s2.URL)

	// Add first connection
	b.Add("conn-1", c1)
	assert.Equal(t, 1, b.Count())

	// Replace with different connection (same ID)
	b.Add("conn-1", c2)
	assert.Equal(t, 1, b.Count()) // Still 1, replaced not added

	// Original connection should be closed
	_, _, err := c1.Reader(context.Background())
	assert.Error(t, err) // Connection should be closed
}

func TestBroadcaster_Remove(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	s := newMockWSserver(t)
	defer s.Close()
	c := connectWS(t, s.URL)
	defer closeConn(c)

	b.Add("conn-1", c)
	assert.Equal(t, 1, b.Count())

	b.Remove("conn-1")
	assert.Equal(t, 0, b.Count())
	assert.False(t, b.HasConnections())

	// Remove non-existent is safe
	b.Remove("conn-unknown")
	assert.Equal(t, 0, b.Count())
}

func TestBroadcaster_WriteJSON_Single(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	s := newCaptureWSserver(t)
	defer s.Close()
	c := connectWS(t, s.URL)
	defer closeConn(c)

	b.Add("conn-1", c)

	// Write message
	err := b.WriteJSON(context.Background(), map[string]string{"type": "test", "data": "hello"})
	require.NoError(t, err)

	// Verify message was received
	time.Sleep(100 * time.Millisecond)
	msgs := s.GetMessages()
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "test")
	assert.Contains(t, msgs[0], "hello")
}

func TestBroadcaster_WriteJSON_Multiple(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	// Create 3 capture servers
	servers := make([]*captureWSServer, 3)
	conns := make([]*websocket.Conn, 3)

	for i := 0; i < 3; i++ {
		servers[i] = newCaptureWSserver(t)
		defer servers[i].Close()

		conns[i] = connectWS(t, servers[i].URL)
		defer closeConn(conns[i])

		b.Add(string(rune('a'+i)), conns[i])
	}

	assert.Equal(t, 3, b.Count())

	// Broadcast to all
	err := b.WriteJSON(context.Background(), map[string]string{"type": "broadcast", "msg": "hello all"})
	require.NoError(t, err)

	// All connections should receive the message
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 3; i++ {
		msgs := servers[i].GetMessages()
		require.Len(t, msgs, 1, "Server %d should have 1 message", i)
		assert.Contains(t, msgs[0], "broadcast")
	}

	// All connections should still be active
	assert.Equal(t, 3, b.Count())
}

func TestBroadcaster_WriteJSON_RemovesFailed(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	s := newMockWSserver(t)
	defer s.Close()
	c := connectWS(t, s.URL)

	b.Add("conn-1", c)

	// Close the connection before writing
	_ = c.Close(websocket.StatusNormalClosure, "")

	// Wait for close to propagate
	time.Sleep(100 * time.Millisecond)

	// Write should fail and remove the dead connection
	err := b.WriteJSON(context.Background(), map[string]string{"type": "test"})
	require.NoError(t, err) // No error - just removes failed conns

	assert.Equal(t, 0, b.Count()) // Connection removed
}

func TestBroadcaster_WriteJSON_NoConnections(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	// Write with no connections should succeed
	err := b.WriteJSON(context.Background(), map[string]string{"type": "test"})
	require.NoError(t, err)
}

func TestBroadcaster_ConcurrentAddRemove(t *testing.T) {
	logger := zerolog.Nop()
	b := wsconn.NewBroadcaster(logger)

	const numOps = 100
	done := make(chan bool, 2)

	// Concurrent adds
	go func() {
		for i := 0; i < numOps; i++ {
			b.Add(string(rune(i)), nil) // nil is safely ignored
		}
		done <- true
	}()

	// Concurrent removes
	go func() {
		for i := 0; i < numOps; i++ {
			b.Remove(string(rune(i)))
		}
		done <- true
	}()

	<-done
	<-done

	// Should not panic or race
}

// Mock servers

func newMockWSserver(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer closeConn(c)

		// Just keep connection alive
		for {
			_, _, err := c.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
}

type captureWSServer struct {
	*httptest.Server
	mu       sync.Mutex
	messages []string
}

func (s *captureWSServer) GetMessages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.messages...)
}

func (s *captureWSServer) addMessage(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
}

func newCaptureWSserver(t *testing.T) *captureWSServer {
	s := &captureWSServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer closeConn(c)

		// Read all messages and store them
		for {
			_, reader, err := c.Reader(context.Background())
			if err != nil {
				return
			}

			buf := make([]byte, 4096)
			n, err := reader.Read(buf)
			if err != nil {
				return
			}

			s.addMessage(string(buf[:n]))
		}
	}))
	return s
}

func connectWS(t *testing.T, url string) *websocket.Conn {
	wsURL := "ws" + url[4:] // http:// -> ws://
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	return c
}

func closeConn(c *websocket.Conn) {
	_ = c.Close(websocket.StatusNormalClosure, "")
}
