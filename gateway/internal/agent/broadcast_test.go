package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/gateway/internal/agent"
)

// TestUserAgent_MultipleConnections tests adding multiple connections to an agent
func TestUserAgent_MultipleConnections(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	agentID := "agent-multi-conn"

	// Create mock websocket connections
	s1 := newMockWSserver(t)
	defer s1.Close()
	c1 := connectWS(t, s1.URL)
	defer closeConn(c1)

	s2 := newMockWSserver(t)
	defer s2.Close()
	c2 := connectWS(t, s2.URL)
	defer closeConn(c2)

	s3 := newMockWSserver(t)
	defer s3.Close()
	c3 := connectWS(t, s3.URL)
	defer closeConn(c3)

	// First message creates agent and adds first connection
	msg := agent.Message{Content: "test", ID: "msg-1", Model: &apitypes.ModelEntry{ID: "gpt-4o"}}
	ok := mgr.SubmitMsg(context.Background(), agentID, "conn-1", c1, msg)
	require.True(t, ok)

	ag := mgr.Get(agentID)
	require.NotNil(t, ag)
	require.Equal(t, 1, ag.ConnCount())

	// Submit more messages with different connections (simulates multi-device)
	msg2 := agent.Message{Content: "test2", ID: "msg-2", Model: &apitypes.ModelEntry{ID: "gpt-4o"}}
	ok = mgr.SubmitMsg(context.Background(), agentID, "conn-2", c2, msg2)
	require.True(t, ok)
	assert.Equal(t, 2, ag.ConnCount())

	msg3 := agent.Message{Content: "test3", ID: "msg-3", Model: &apitypes.ModelEntry{ID: "gpt-4o"}}
	ok = mgr.SubmitMsg(context.Background(), agentID, "conn-3", c3, msg3)
	require.True(t, ok)
	assert.Equal(t, 3, ag.ConnCount())
}

// TestUserAgent_RemoveConnection tests removing connections from an agent
func TestUserAgent_RemoveConnection(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	agentID := "agent-remove"

	s1 := newMockWSserver(t)
	defer s1.Close()
	c1 := connectWS(t, s1.URL)
	defer closeConn(c1)

	s2 := newMockWSserver(t)
	defer s2.Close()
	c2 := connectWS(t, s2.URL)
	defer closeConn(c2)

	// Create agent and add connections via SubmitMsg
	msg := agent.Message{Content: "test", ID: "msg-1", Model: &apitypes.ModelEntry{ID: "gpt-4o"}}
	mgr.SubmitMsg(context.Background(), agentID, "conn-1", c1, msg)
	mgr.SubmitMsg(context.Background(), agentID, "conn-2", c2, msg)

	ag := mgr.Get(agentID)
	require.NotNil(t, ag)
	assert.Equal(t, 2, ag.ConnCount())

	// Remove first connection
	ag.RemoveConn("conn-1")
	assert.Equal(t, 1, ag.ConnCount())

	// Remove second connection
	ag.RemoveConn("conn-2")
	assert.Equal(t, 0, ag.ConnCount())
}

// TestUserAgent_RemoveNonExistent tests removing a connection that doesn't exist
func TestUserAgent_RemoveNonExistent(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	agentID := "agent-noexist"

	// Create agent
	s := newMockWSserver(t)
	defer s.Close()
	c := connectWS(t, s.URL)
	defer closeConn(c)

	msg := agent.Message{Content: "test", ID: "msg-1", Model: &apitypes.ModelEntry{ID: "gpt-4o"}}
	mgr.SubmitMsg(context.Background(), agentID, "conn-1", c, msg)

	ag := mgr.Get(agentID)
	require.NotNil(t, ag)
	assert.Equal(t, 1, ag.ConnCount())

	// Remove non-existent connection ID is safe
	ag.RemoveConn("conn-unknown")
	assert.Equal(t, 1, ag.ConnCount()) // Original still there
}

// TestUserAgent_ReplaceConnection tests replacing a connection with same ID
func TestUserAgent_ReplaceConnection(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	agentID := "agent-replace"

	s1 := newMockWSserver(t)
	defer s1.Close()
	c1 := connectWS(t, s1.URL)

	s2 := newMockWSserver(t)
	defer s2.Close()
	c2 := connectWS(t, s2.URL)

	// Create agent with first connection
	msg := agent.Message{Content: "test", ID: "msg-1", Model: &apitypes.ModelEntry{ID: "gpt-4o"}}
	mgr.SubmitMsg(context.Background(), agentID, "conn-1", c1, msg)

	ag := mgr.Get(agentID)
	require.NotNil(t, ag)
	assert.Equal(t, 1, ag.ConnCount())

	// Replace with same ID but different connection (via SubmitMsg)
	mgr.SubmitMsg(context.Background(), agentID, "conn-1", c2, msg)

	// Should still be 1 (replaced, not added)
	assert.Equal(t, 1, ag.ConnCount())

	// Old connection should be closed
	_, _, err := c1.Reader(context.Background())
	assert.Error(t, err) // c1 should be closed
}

// TestUserAgent_StatusIncludesConnCount tests that GetStatus includes connection count
func TestUserAgent_StatusIncludesConnCount(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	agentID := "agent-status"

	s1 := newMockWSserver(t)
	defer s1.Close()
	c1 := connectWS(t, s1.URL)
	defer closeConn(c1)

	s2 := newMockWSserver(t)
	defer s2.Close()
	c2 := connectWS(t, s2.URL)
	defer closeConn(c2)

	// Create agent and add connections via SubmitMsg
	msg := agent.Message{Content: "test", ID: "msg-1", Model: &apitypes.ModelEntry{ID: "gpt-4o"}}
	mgr.SubmitMsg(context.Background(), agentID, "conn-1", c1, msg)
	mgr.SubmitMsg(context.Background(), agentID, "conn-2", c2, msg)

	ag := mgr.Get(agentID)
	require.NotNil(t, ag)

	status := ag.GetStatus()
	assert.Equal(t, "agent-status", status["agent_id"])
	assert.Equal(t, 2, status["conn_count"])
	assert.Equal(t, true, status["active"])
}

// TestUserAgent_ConcurrentConnectionOps tests thread safety of connection operations
func TestUserAgent_ConcurrentConnectionOps(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	agentID := "agent-concurrent"

	// Create agent first
	msg := agent.Message{Content: "test", ID: "msg-1", Model: &apitypes.ModelEntry{ID: "gpt-4o"}}
	mgr.SubmitMsg(context.Background(), agentID, "conn-init", nil, msg)

	ag := mgr.Get(agentID)
	require.NotNil(t, ag)

	const numOps = 50
	done := make(chan bool, 2)

	// Concurrent SubmitMsg (which adds connections)
	go func() {
		for i := 0; i < numOps; i++ {
			mgr.SubmitMsg(context.Background(), agentID, "conn-add-"+string(rune(i)), nil, msg)
		}
		done <- true
	}()

	// Concurrent removes
	go func() {
		for i := 0; i < numOps; i++ {
			ag.RemoveConn("conn-remove-" + string(rune(i)))
		}
		done <- true
	}()

	<-done
	<-done

	// Should not panic or race
}

// Helper functions

func newMockWSserver(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer closeConn(c)

		// Keep connection alive
		for {
			_, _, err := c.Read(context.Background())
			if err != nil {
				return
			}
		}
	}))
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
