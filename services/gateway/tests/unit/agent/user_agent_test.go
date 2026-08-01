package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/services/gateway/internal/agent"
	"github.com/radch-ai/ava/services/gateway/internal/wsconn"
)

func TestUserAgent_SubmitMsg_Success(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	userID := "user-1"
	agentID := "agent-start-test"
	conn := wsconn.NewWrapper(nil)

	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, agent.Message{
		Content: "test message",
		ID:      "msg-1",
		Model:   "gpt-4o",
	})
	require.True(t, ok)

	// Verify agent was created
	ag := mgr.Get(userID, agentID)
	require.NotNil(t, ag)
}

func TestUserAgent_ConcurrentSubmit(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	userID := "user-1"
	agentID := "agent-concurrent-test"
	conn := wsconn.NewWrapper(nil)

	// Create agent first
	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, agent.Message{
		Content: "initial",
		ID:      "msg-0",
		Model:   "gpt-4o",
	})
	require.True(t, ok)

	const numGoroutines = 10
	const numMessagesPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numMessagesPerGoroutine; j++ {
				mgr.SubmitMsg(context.Background(), userID, agentID, conn, agent.Message{
					Content: "concurrent message",
					ID:      "msg-concurrent",
					Model:   "gpt-4o",
				})
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)
}

func TestUserAgent_IdleTimeout_TriggersExit(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	userID := "user-1"
	agentID := "agent-idle-exit-test"
	conn := wsconn.NewWrapper(nil)

	// Create agent
	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, agent.Message{
		Content: "test",
		ID:      "msg-1",
		Model:   "gpt-4o",
	})
	require.True(t, ok)

	// Agent should exist
	ag := mgr.Get(userID, agentID)
	require.NotNil(t, ag)
}

func TestUserAgent_GetStatus(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	userID := "user-1"
	agentID := "agent-status-test"
	conn := wsconn.NewWrapper(nil)

	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, agent.Message{
		Content: "test",
		ID:      "msg-1",
		Model:   "gpt-4o",
	})
	require.True(t, ok)

	ag := mgr.Get(userID, agentID)
	require.NotNil(t, ag)

	status := ag.GetStatus()
	assert.Equal(t, "agent-status-test", status["agent_id"])
	assert.Equal(t, "user-1", status["user_id"])
	assert.Equal(t, true, status["active"])
	assert.Equal(t, "gpt-4o", status["model"])
}

func TestUserAgent_Interrupt(t *testing.T) {
	logger := zerolog.Nop()
	mgr := agent.NewUserAgentManager(logger, &agent.UserAgentManagerOptions{})

	userID := "user-1"
	agentID := "agent-interrupt-test"
	conn := wsconn.NewWrapper(nil)

	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, agent.Message{
		Content: "test",
		ID:      "msg-1",
		Model:   "gpt-4o",
	})
	require.True(t, ok)

	ag := mgr.Get(userID, agentID)
	require.NotNil(t, ag)

	// Interrupt should not panic
	ag.Interrupt()
}
