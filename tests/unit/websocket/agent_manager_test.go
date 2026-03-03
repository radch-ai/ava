package websocket_test

import (
	"context"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/websocket"
	"github.com/nevaria-ai/gateway/internal/wsconn"
	"github.com/rs/zerolog"
)

func TestAgentManagerCreation(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	if mgr == nil {
		t.Fatal("NewAgentManager returned nil")
	}
}

func TestGet(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-123"
	agentID := "test-agent-456"
	conn := wsconn.NewWrapper(nil)

	// Get returns nil before agent is created
	ag := mgr.Get(userID, agentID)
	if ag != nil {
		t.Error("Expected nil before agent creation")
	}

	// Create agent via Submit
	msg := agent.Message{Content: "test", ID: "msg-1"}
	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg)
	if !ok {
		t.Fatal("Submit failed")
	}

	// Get returns the agent after creation
	ag = mgr.Get(userID, agentID)
	if ag == nil {
		t.Error("Expected agent after Submit")
	}
}

func TestRemove(t *testing.T) {
	logger := zerolog.Nop()

	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-123"
	agentID := "test-agent-456"
	conn := wsconn.NewWrapper(nil)

	// Create agent via Submit
	msg := agent.Message{Content: "test", ID: "msg-1"}
	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg)
	if !ok {
		t.Fatal("Submit failed")
	}

	// Verify agent exists
	ag := mgr.Get(userID, agentID)
	if ag == nil {
		t.Fatal("Expected agent before removal")
	}

	mgr.Remove(userID, agentID)

	// After removal, Get returns nil
	ag = mgr.Get(userID, agentID)
	if ag != nil {
		t.Error("Expected nil after removal")
	}
}

func TestAgentKeyCompositeKey(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "user-123"
	agentID := "agent-456"
	conn := wsconn.NewWrapper(nil)

	// Create first agent
	msg := agent.Message{Content: "test", ID: "msg-1"}
	_ = mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg)
	ag1 := mgr.Get(userID, agentID)
	if ag1 == nil {
		t.Fatal("Expected agent")
	}

	// Same key returns same agent
	_ = mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg)
	ag2 := mgr.Get(userID, agentID)
	if ag1 != ag2 {
		t.Error("Expected same agent for same (userID, agentID)")
	}

	// Different agentID creates new agent
	_ = mgr.SubmitMsg(context.Background(), userID, "agent-789", conn, msg)
	ag3 := mgr.Get(userID, "agent-789")
	if ag1 == ag3 {
		t.Error("Expected different agent for different agentID")
	}

	// Different userID creates new agent
	_ = mgr.SubmitMsg(context.Background(), "user-456", agentID, conn, msg)
	ag4 := mgr.Get("user-456", agentID)
	if ag1 == ag4 {
		t.Error("Expected different agent for different userID")
	}
}

func TestSubmit(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-submit"
	agentID := "test-agent-submit"
	conn := wsconn.NewWrapper(nil)

	msg := agent.Message{Content: "test message", ID: "msg-1"}

	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg)
	if !ok {
		t.Error("Expected Submit to return true for active agent")
	}
}

func TestSubmitAfterStop(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-stop"
	agentID := "test-agent-stop"
	conn := wsconn.NewWrapper(nil)

	// Create agent via SubmitMsg
	msg := agent.Message{Content: "first message", ID: "msg-1"}
	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg)
	if !ok {
		t.Fatal("Expected first SubmitMsg to return true")
	}

	// Get the agent and stop it
	ag := mgr.Get(userID, agentID)
	if ag == nil {
		t.Fatal("Get returned nil")
	}
	ag.Stop()

	// Wait for exit to propagate (OnExit deletes lane)
	time.Sleep(50 * time.Millisecond)

	// SubmitMsg should create a NEW agent (lane was deleted on exit)
	msg2 := agent.Message{Content: "after stop", ID: "msg-2"}
	ok = mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg2)
	if !ok {
		t.Error("Expected SubmitMsg to create new agent and return true")
	}

	// Verify new agent was created
	ag2 := mgr.Get(userID, agentID)
	if ag2 == nil {
		t.Error("Expected new agent to exist")
	}
	if ag == ag2 {
		t.Error("Expected different agent instance")
	}
}

func TestSubmitCreatesAgent(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-create"
	agentID := "test-agent-create"
	conn := wsconn.NewWrapper(nil)

	// Submit without prior agent should create agent
	msg := agent.Message{Content: "create and submit", ID: "msg-3"}
	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg)
	if !ok {
		t.Error("Expected Submit to create agent and return true")
	}

	// Verify agent was created
	ag := mgr.Get(userID, agentID)
	if ag == nil {
		t.Error("Expected agent to be created by Submit")
	}
}

func TestSubmitResetsDeadLane(t *testing.T) {
	logger := zerolog.Nop()
	mgr := websocket.NewAgentManager(logger, nil)

	userID := "test-user-reset"
	agentID := "test-agent-reset"
	conn := wsconn.NewWrapper(nil)

	// Create agent via Submit
	msg := agent.Message{Content: "first", ID: "msg-1"}
	ok := mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg)
	if !ok {
		t.Fatal("Expected first Submit to return true")
	}

	// Get and stop the agent
	ag := mgr.Get(userID, agentID)
	if ag == nil {
		t.Fatal("Expected agent to exist")
	}
	ag.Stop()

	// Wait for exit
	time.Sleep(50 * time.Millisecond)

	// Submit should create a NEW agent (lane was dead)
	msg2 := agent.Message{Content: "second", ID: "msg-2"}
	ok = mgr.SubmitMsg(context.Background(), userID, agentID, conn, msg2)
	if !ok {
		t.Error("Expected Submit to create new agent after previous one exited")
	}

	// Verify new agent was created
	ag2 := mgr.Get(userID, agentID)
	if ag2 == nil {
		t.Error("Expected new agent to be created")
	}
	if ag == ag2 {
		t.Error("Expected different agent instance")
	}
}
