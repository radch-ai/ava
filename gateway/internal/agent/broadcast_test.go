package agent_test

// Broadcaster-facing pins through the manager's queue-state APIs:
// connections attach, count, replace, and remove — all against the
// manager-owned state, so they work with or without a live worker.

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

	"github.com/radch-ai/ava/gateway/internal/agent"
)

// submitConn subscribes one connection to the lane's feed; these tests are
// about connection bookkeeping, so no prompt is admitted.
func submitConn(mgr *agent.UserAgentManager, agentID, connID string, conn *websocket.Conn) {
	subscribe(mgr, agentID, connID, conn)
}

func TestMultipleConnectionsAttach(t *testing.T) {
	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{GetAgentThread: emptyThread})

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

	agentID := "agent-multi-conn"
	submitConn(mgr, agentID, "conn-1", c1)
	assert.Equal(t, 1, agent.ConnCount(mgr, agentID))

	submitConn(mgr, agentID, "conn-2", c2)
	assert.Equal(t, 2, agent.ConnCount(mgr, agentID))

	submitConn(mgr, agentID, "conn-3", c3)
	assert.Equal(t, 3, agent.ConnCount(mgr, agentID))
}

func TestRemoveConnectionDropsFromState(t *testing.T) {
	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{GetAgentThread: emptyThread})

	s1 := newMockWSserver(t)
	defer s1.Close()
	c1 := connectWS(t, s1.URL)
	defer closeConn(c1)

	s2 := newMockWSserver(t)
	defer s2.Close()
	c2 := connectWS(t, s2.URL)
	defer closeConn(c2)

	agentID := "agent-remove"
	submitConn(mgr, agentID, "conn-1", c1)
	submitConn(mgr, agentID, "conn-2", c2)
	require.Equal(t, 2, agent.ConnCount(mgr, agentID))

	mgr.RemoveConn(agentID, "conn-1")
	assert.Equal(t, 1, agent.ConnCount(mgr, agentID))

	mgr.RemoveConn(agentID, "conn-2")
	assert.Equal(t, 0, agent.ConnCount(mgr, agentID))
}

func TestRemoveNonExistentConnectionIsSafe(t *testing.T) {
	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{GetAgentThread: emptyThread})

	s := newMockWSserver(t)
	defer s.Close()
	c := connectWS(t, s.URL)
	defer closeConn(c)

	agentID := "agent-noexist"
	submitConn(mgr, agentID, "conn-1", c)
	require.Equal(t, 1, agent.ConnCount(mgr, agentID))

	mgr.RemoveConn(agentID, "conn-unknown")
	assert.Equal(t, 1, agent.ConnCount(mgr, agentID))
}

func TestReplaceConnectionWithSameID(t *testing.T) {
	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{GetAgentThread: emptyThread})

	s1 := newMockWSserver(t)
	defer s1.Close()
	c1 := connectWS(t, s1.URL)

	s2 := newMockWSserver(t)
	defer s2.Close()
	c2 := connectWS(t, s2.URL)

	agentID := "agent-replace"
	submitConn(mgr, agentID, "conn-1", c1)
	require.Equal(t, 1, agent.ConnCount(mgr, agentID))

	// Same conn id, new connection: replaced, not added.
	submitConn(mgr, agentID, "conn-1", c2)
	assert.Equal(t, 1, agent.ConnCount(mgr, agentID))

	// The replaced connection is closed.
	_, _, err := c1.Reader(context.Background())
	assert.Error(t, err)
}

func TestConcurrentConnectionOps(t *testing.T) {
	mgr := agent.NewUserAgentManager(zerolog.Nop(), &agent.UserAgentManagerOptions{GetAgentThread: emptyThread})

	agentID := "agent-concurrent"
	submitConn(mgr, agentID, "conn-init", nil)

	const numOps = 50
	done := make(chan bool, 2)

	go func() {
		for i := range numOps {
			submitConn(mgr, agentID, "conn-add-"+string(rune(i)), nil)
		}
		done <- true
	}()

	go func() {
		for i := range numOps {
			mgr.RemoveConn(agentID, "conn-remove-"+string(rune(i)))
		}
		done <- true
	}()

	<-done
	<-done

	// Should not panic or race.
}

func newMockWSserver(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer closeConn(c)

		// Keep connection alive
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
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
