//go:build integration

package agent_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type IntegrationMockWSConn struct {
	mock.Mock
	mu       sync.Mutex
	writes   []interface{}
	writeErr error
}

func (m *IntegrationMockWSConn) WriteJSON(v interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return m.writeErr
	}
	m.writes = append(m.writes, v)
	return nil
}

func (m *IntegrationMockWSConn) SetWriteError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeErr = err
}

func (m *IntegrationMockWSConn) GetWrites() []interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]interface{}, len(m.writes))
	copy(result, m.writes)
	return result
}

type IntegrationMockEinoRunner struct {
	mock.Mock
	events       []agent.EinoEvent
	responseText string
	queryErr     error
	mu           sync.Mutex
}

func (m *IntegrationMockEinoRunner) Query(ctx context.Context, input string, handler interface{}) (<-chan agent.EinoEvent, error) {
	m.Called(ctx, input, handler)

	m.mu.Lock()
	events := make([]agent.EinoEvent, len(m.events))
	copy(events, m.events)
	responseText := m.responseText
	queryErr := m.queryErr
	m.mu.Unlock()

	if queryErr != nil {
		return nil, queryErr
	}

	ch := make(chan agent.EinoEvent, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	if responseText != "" {
		ch <- agent.EinoEvent{Type: agent.EinoEventTypeText, Content: responseText}
	}
	ch <- agent.EinoEvent{Type: agent.EinoEventTypeEnd}
	close(ch)

	return ch, nil
}

func (m *IntegrationMockEinoRunner) SetEvents(events []agent.EinoEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = events
}

func (m *IntegrationMockEinoRunner) SetResponseText(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responseText = text
}

func (m *IntegrationMockEinoRunner) SetQueryError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryErr = err
}

type IntegrationMockAgentManager struct {
	mock.Mock
	evicted []string
	mu      sync.Mutex
}

func (m *IntegrationMockAgentManager) Evict(agentID string) {
	m.mu.Lock()
	m.evicted = append(m.evicted, agentID)
	m.mu.Unlock()
	m.Called(agentID)
}

func (m *IntegrationMockAgentManager) WasEvicted(agentID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range m.evicted {
		if id == agentID {
			return true
		}
	}
	return false
}

type IntegrationMockHistorySaver struct {
	mock.Mock
	saved     bool
	saveCount int
	lastHist  []agent.AgentMessage
	mu        sync.Mutex
}

func (m *IntegrationMockHistorySaver) SaveHistory(ctx context.Context, sessionID string, history []agent.AgentMessage) error {
	m.mu.Lock()
	m.saved = true
	m.saveCount++
	m.lastHist = history
	m.mu.Unlock()
	args := m.Called(ctx, sessionID, history)
	return args.Error(0)
}

func (m *IntegrationMockHistorySaver) WasSaved() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved
}

func (m *IntegrationMockHistorySaver) SaveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveCount
}

func (m *IntegrationMockHistorySaver) GetLastHistory() []agent.AgentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastHist
}

type IntegrationMockNotifier struct {
	mock.Mock
	notified      bool
	notifications []struct {
		userID  string
		agentID string
		msgID   string
		message string
	}
	mu sync.Mutex
}

func (m *IntegrationMockNotifier) Notify(userID, agentID, msgID, message string) error {
	m.mu.Lock()
	m.notified = true
	m.notifications = append(m.notifications, struct {
		userID  string
		agentID string
		msgID   string
		message string
	}{userID, agentID, msgID, message})
	m.mu.Unlock()
	args := m.Called(userID, agentID, msgID, message)
	return args.Error(0)
}

func (m *IntegrationMockNotifier) WasNotified() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.notified
}

func (m *IntegrationMockNotifier) GetNotifications() []struct {
	userID  string
	agentID string
	msgID   string
	message string
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]struct {
		userID  string
		agentID string
		msgID   string
		message string
	}, len(m.notifications))
	copy(result, m.notifications)
	return result
}

type IntegrationMockAgentDB struct {
	mu     sync.Mutex
	config map[string]*agent.AgentConfig
}

func NewIntegrationMockAgentDB() *IntegrationMockAgentDB {
	return &IntegrationMockAgentDB{
		config: make(map[string]*agent.AgentConfig),
	}
}

func (m *IntegrationMockAgentDB) key(userID, agentID string) string {
	return fmt.Sprintf("%s:%s", userID, agentID)
}

func (m *IntegrationMockAgentDB) GetAgentConfig(userID, agentID string) (*agent.AgentConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.key(userID, agentID)
	cfg, exists := m.config[key]
	if !exists {
		return nil, errors.New("agent not found")
	}
	return cfg, nil
}

func (m *IntegrationMockAgentDB) SetAgentConfig(cfg *agent.AgentConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config[m.key(cfg.UserID, cfg.AgentID)] = cfg
}

func TestEinoAgent_FullFlow(t *testing.T) {
	t.Run("creates agent and submits message", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Hello! How can I help you today?"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, "Hello, agent!", mock.Anything).Return(nil)

		mockSaver := &IntegrationMockHistorySaver{}
		mockSaver.On("SaveHistory", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-full-flow",
			UserID:       "user-123",
			SystemPrompt: "You are a helpful assistant",
			Runner:       mockRunner,
			HistorySaver: mockSaver,
			IdleTimeout:  2 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		require.NotNil(t, ag)
		assert.Equal(t, "agent-full-flow", ag.GetAgentID())
		assert.Equal(t, "user-123", ag.GetUserID())

		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{
			Message: "Hello, agent!",
			Conn:    mockConn,
			MsgID:   "msg-001",
		})

		time.Sleep(200 * time.Millisecond)

		mockRunner.AssertExpectations(t)
		assert.True(t, mockSaver.WasSaved(), "History should have been saved")
		assert.Greater(t, len(mockSaver.GetLastHistory()), 0, "History should contain messages")
	})

	t.Run("processes multiple sequential messages", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-sequential",
			UserID:      "user-456",
			Runner:      mockRunner,
			IdleTimeout: 3 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		for i := 1; i <= 3; i++ {
			ag.Submit(agent.Task{
				Message: fmt.Sprintf("Message %d", i),
				Conn:    mockConn,
				MsgID:   fmt.Sprintf("msg-%d", i),
			})
			time.Sleep(100 * time.Millisecond)
		}

		time.Sleep(200 * time.Millisecond)
		mockRunner.AssertExpectations(t)
	})

	t.Run("handles runner errors gracefully", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetQueryError(errors.New("LLM service unavailable"))
		mockRunner.On("Query", mock.Anything, "This should fail", mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-error",
			UserID:      "user-789",
			Runner:      mockRunner,
			IdleTimeout: 2 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{
			Message: "This should fail",
			Conn:    mockConn,
			MsgID:   "msg-err",
		})

		time.Sleep(100 * time.Millisecond)
		mockRunner.AssertExpectations(t)
	})
}

func TestAgentManager_FullFlow(t *testing.T) {
	t.Run("GetAgent loads from DB and caches", func(t *testing.T) {
		mockDB := NewIntegrationMockAgentDB()
		mockDB.SetAgentConfig(&agent.AgentConfig{
			AgentID:      "agent-mgr-1",
			UserID:       "user-mgr",
			SystemPrompt: "System prompt for agent",
		})

		mockLLM := &agent.MockLLMClient{
			Responses: []agent.MockResponse{
				{Message: agent.NewTextMessage(agent.RoleAssistant, "Response from agent")},
			},
		}

		mgr := agent.NewAgentManager(mockDB, mockLLM, nil, nil)

		a1 := mgr.GetAgent("user-mgr", "agent-mgr-1")
		require.NotNil(t, a1)

		a2 := mgr.GetAgent("user-mgr", "agent-mgr-1")
		require.NotNil(t, a2)

		assert.Equal(t, a1, a2, "Should return same cached agent instance")
	})

	t.Run("GetAgent returns nil for non-existent agent", func(t *testing.T) {
		mockDB := NewIntegrationMockAgentDB()
		mockLLM := &agent.MockLLMClient{}

		mgr := agent.NewAgentManager(mockDB, mockLLM, nil, nil)

		a := mgr.GetAgent("user-unknown", "agent-unknown")
		assert.Nil(t, a)
	})

	t.Run("Evict removes agent from cache", func(t *testing.T) {
		mockDB := NewIntegrationMockAgentDB()
		mockDB.SetAgentConfig(&agent.AgentConfig{
			AgentID:      "agent-evict",
			UserID:       "user-evict",
			SystemPrompt: "Test prompt",
		})

		mockLLM := &agent.MockLLMClient{
			Responses: []agent.MockResponse{
				{Message: agent.NewTextMessage(agent.RoleAssistant, "Response")},
			},
		}

		mgr := agent.NewAgentManager(mockDB, mockLLM, nil, nil)

		a1 := mgr.GetAgent("user-evict", "agent-evict")
		require.NotNil(t, a1)

		mgr.Evict("agent-evict")

		a2 := mgr.GetAgent("user-evict", "agent-evict")
		require.NotNil(t, a2)
		assert.NotEqual(t, a1, a2, "Should be different instances after eviction")
	})

	t.Run("agent state is accessible after creation", func(t *testing.T) {
		mockDB := NewIntegrationMockAgentDB()
		mockDB.SetAgentConfig(&agent.AgentConfig{
			AgentID:      "agent-state",
			UserID:       "user-state",
			SystemPrompt: "System prompt",
		})

		mockLLM := &agent.MockLLMClient{}

		mgr := agent.NewAgentManager(mockDB, mockLLM, nil, nil)

		a := mgr.GetAgent("user-state", "agent-state")
		require.NotNil(t, a)

		state := a.State()
		assert.Equal(t, "agent-state", state.AgentID)
		assert.Equal(t, "System prompt", state.SystemPrompt)
	})

	t.Run("agent can add and remove tools", func(t *testing.T) {
		mockDB := NewIntegrationMockAgentDB()
		mockDB.SetAgentConfig(&agent.AgentConfig{
			AgentID:      "agent-tools",
			UserID:       "user-tools",
			SystemPrompt: "Test prompt",
		})

		mockLLM := &agent.MockLLMClient{}

		mgr := agent.NewAgentManager(mockDB, mockLLM, nil, nil)

		a := mgr.GetAgent("user-tools", "agent-tools")
		require.NotNil(t, a)

		tool := agent.Tool{
			Name:        "test_tool",
			Description: "A test tool",
			Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
				return &agent.ToolResult{
					Content: []agent.ContentBlock{agent.TextContent{Text: "ok"}},
				}, nil
			},
		}

		a.AddTools(tool)

		state := a.State()
		assert.Len(t, state.Tools, 1)
		assert.Equal(t, "test_tool", state.Tools[0].Name)

		a.RemoveTools("test_")

		state = a.State()
		assert.Len(t, state.Tools, 0)
	})
}

func TestConnectionAgnosticStreaming(t *testing.T) {
	t.Run("streams to live connection", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Streaming response to live connection"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, "Stream to me", mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-live-conn",
			UserID:      "user-live",
			Runner:      mockRunner,
			IdleTimeout: 2 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{
			Message: "Stream to me",
			Conn:    mockConn,
			MsgID:   "msg-live",
		})

		time.Sleep(200 * time.Millisecond)

		writes := mockConn.GetWrites()
		assert.Greater(t, len(writes), 0, "Should have written to live connection")

		var foundTextDelta bool
		for _, w := range writes {
			if pe, ok := w.(agent.ProgressEvent); ok {
				if pe.Type == "text_delta" {
					foundTextDelta = true
					break
				}
			}
		}
		assert.True(t, foundTextDelta, "Should have sent text_delta event")
		mockRunner.AssertExpectations(t)
	})

	t.Run("handles nil connection with notification path", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response for nil connection"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, "No connection here", mock.Anything).Return(nil)

		mockNotifier := &IntegrationMockNotifier{}
		mockNotifier.On("Notify", "user-nil", "agent-nil-conn", "msg-nil", mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-nil-conn",
			UserID:      "user-nil",
			Runner:      mockRunner,
			Notifier:    mockNotifier,
			IdleTimeout: 2 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)

		ag.Submit(agent.Task{
			Message: "No connection here",
			Conn:    nil,
			MsgID:   "msg-nil",
		})

		time.Sleep(200 * time.Millisecond)

		assert.True(t, mockNotifier.WasNotified(), "Should have sent notification for nil connection")
		notifications := mockNotifier.GetNotifications()
		assert.Greater(t, len(notifications), 0, "Should have at least one notification")
		assert.Equal(t, "user-nil", notifications[0].userID)
		assert.Equal(t, "agent-nil-conn", notifications[0].agentID)
		assert.Equal(t, "msg-nil", notifications[0].msgID)
		mockRunner.AssertExpectations(t)
		mockNotifier.AssertExpectations(t)
	})

	t.Run("sends notification when connection dies during streaming", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response after connection deaths"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, "Connection will die", mock.Anything).Return(nil)

		mockNotifier := &IntegrationMockNotifier{}
		mockNotifier.On("Notify", "user-death", "agent-conn-death", "msg-death", mock.Anything).Return(nil)

		mockSaver := &IntegrationMockHistorySaver{}
		mockSaver.On("SaveHistory", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-conn-death",
			UserID:       "user-death",
			Runner:       mockRunner,
			Notifier:     mockNotifier,
			HistorySaver: mockSaver,
			IdleTimeout:  2 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}
		mockConn.SetWriteError(errors.New("connection died"))

		ag.Submit(agent.Task{
			Message: "Connection will die",
			Conn:    mockConn,
			MsgID:   "msg-death",
		})

		time.Sleep(200 * time.Millisecond)

		assert.True(t, mockSaver.WasSaved(), "History should still be saved")
		assert.True(t, mockNotifier.WasNotified(), "Should have sent notification")
		mockRunner.AssertExpectations(t)
	})

	t.Run("does not send notification when connection stays alive", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response to healthy connection"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, "Healthy connection", mock.Anything).Return(nil)

		mockNotifier := &IntegrationMockNotifier{}
		mockSaver := &IntegrationMockHistorySaver{}
		mockSaver.On("SaveHistory", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-healthy-conn",
			UserID:       "user-healthy",
			Runner:       mockRunner,
			Notifier:     mockNotifier,
			HistorySaver: mockSaver,
			IdleTimeout:  2 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{
			Message: "Healthy connection",
			Conn:    mockConn,
			MsgID:   "msg-healthy",
		})

		time.Sleep(200 * time.Millisecond)

		assert.True(t, mockSaver.WasSaved(), "History should be saved")
		assert.False(t, mockNotifier.WasNotified(), "Should NOT have sent notification for live connection")
		mockRunner.AssertExpectations(t)
	})
}

func TestEinoAgent_IdleEviction(t *testing.T) {
	t.Run("evicts agent after idle timeout", func(t *testing.T) {
		mockManager := &IntegrationMockAgentManager{}
		mockManager.On("Evict", "agent-idle-evict").Return()

		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Quick response"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, "Trigger processing", mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-idle-evict",
			UserID:      "user-idle",
			Runner:      mockRunner,
			Manager:     mockManager,
			IdleTimeout: 100 * time.Millisecond,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{
			Message: "Trigger processing",
			Conn:    mockConn,
			MsgID:   "msg-trigger",
		})

		time.Sleep(300 * time.Millisecond)

		assert.True(t, mockManager.WasEvicted("agent-idle-evict"), "Agent should have been evicted after idle timeout")
		mockManager.AssertExpectations(t)
		mockRunner.AssertExpectations(t)
	})

	t.Run("resets idle timer after each message", func(t *testing.T) {
		mockManager := &IntegrationMockAgentManager{}
		mockManager.On("Evict", "agent-reset-timer").Return()

		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-reset-timer",
			UserID:      "user-reset",
			Manager:     mockManager,
			Runner:      mockRunner,
			IdleTimeout: 200 * time.Millisecond,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{
			Message: "Message 1",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(100 * time.Millisecond)
		assert.False(t, mockManager.WasEvicted("agent-reset-timer"), "Should not evict while messages are being processed")

		time.Sleep(300 * time.Millisecond)
		assert.True(t, mockManager.WasEvicted("agent-reset-timer"), "Should evict after idle timeout")
	})
}

func TestEinoAgent_ContextCancellation(t *testing.T) {
	t.Run("stops processing on context cancellation", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Should be cancelled"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, "Will be cancelled", mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-cancel",
			UserID:      "user-cancel",
			Runner:      mockRunner,
			IdleTimeout: 10 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{
			Message: "Will be cancelled",
			Conn:    mockConn,
			MsgID:   "msg-cancel",
		})

		time.Sleep(50 * time.Millisecond)

		ag.Cancel()

		time.Sleep(100 * time.Millisecond)
		mockRunner.AssertExpectations(t)
	})
}

func TestEinoAgent_HistoryPersistence(t *testing.T) {
	t.Run("saves conversation history after each turn", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockSaver := &IntegrationMockHistorySaver{}
		mockSaver.On("SaveHistory", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-history",
			UserID:       "user-history",
			Runner:       mockRunner,
			HistorySaver: mockSaver,
			IdleTimeout:  2 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{
			Message: "First message",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(150 * time.Millisecond)

		ag.Submit(agent.Task{
			Message: "Second message",
			Conn:    mockConn,
			MsgID:   "msg-2",
		})

		time.Sleep(150 * time.Millisecond)

		assert.Equal(t, 2, mockSaver.SaveCount(), "Should have saved history twice")
		mockRunner.AssertExpectations(t)
	})

	t.Run("continues without history saver if not configured", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, "No saver", mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-no-saver",
			UserID:       "user-no-saver",
			Runner:       mockRunner,
			HistorySaver: nil,
			IdleTimeout:  2 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{
			Message: "No saver",
			Conn:    mockConn,
			MsgID:   "msg-no-saver",
		})

		time.Sleep(150 * time.Millisecond)
		mockRunner.AssertExpectations(t)
	})
}

func TestEinoAgent_MessageBuffering(t *testing.T) {
	t.Run("drains multiple queued messages", func(t *testing.T) {
		mockRunner := &IntegrationMockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Combined response"},
			{Type: agent.EinoEventTypeEnd},
		})
		mockRunner.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockSaver := &IntegrationMockHistorySaver{}
		mockSaver.On("SaveHistory", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-buffer",
			UserID:       "user-buffer",
			Runner:       mockRunner,
			HistorySaver: mockSaver,
			IdleTimeout:  2 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &IntegrationMockWSConn{}

		ag.Submit(agent.Task{Message: "Message 1", Conn: mockConn, MsgID: "msg-1"})
		ag.Submit(agent.Task{Message: "Message 2", Conn: mockConn, MsgID: "msg-2"})
		ag.Submit(agent.Task{Message: "Message 3", Conn: mockConn, MsgID: "msg-3"})

		time.Sleep(200 * time.Millisecond)

		mockRunner.AssertExpectations(t)
		assert.True(t, mockSaver.WasSaved(), "History should have been saved")
	})
}

func TestAgent_EventSubscription(t *testing.T) {
	t.Run("agent emits events to subscribers", func(t *testing.T) {
		ag := agent.NewAgent(agent.AgentOptions{
			ID:           "agent-events",
			UserID:       "user-events",
			AgentID:      "agent-events",
			SystemPrompt: "Test agent",
		})

		subID, events := ag.Subscribe()
		defer ag.Unsubscribe(subID)

		assert.NotEmpty(t, subID)
		assert.NotNil(t, events)

		ag.AddTools(agent.Tool{
			Name:        "test_tool",
			Description: "Test tool",
		})

		state := ag.State()
		assert.Len(t, state.Tools, 1)
	})

	t.Run("multiple subscribers receive events", func(t *testing.T) {
		ag := agent.NewAgent(agent.AgentOptions{
			ID:           "agent-multi-sub",
			UserID:       "user-multi",
			AgentID:      "agent-multi-sub",
			SystemPrompt: "Test",
		})

		subID1, events1 := ag.Subscribe()
		subID2, events2 := ag.Subscribe()
		defer ag.Unsubscribe(subID1)
		defer ag.Unsubscribe(subID2)

		assert.NotNil(t, events1)
		assert.NotNil(t, events2)
		assert.NotEqual(t, subID1, subID2)
	})
}

func TestAgent_LaneOperations(t *testing.T) {
	t.Run("agent has functional lane for message queuing", func(t *testing.T) {
		ag := agent.NewAgent(agent.AgentOptions{
			ID:           "agent-lane",
			UserID:       "user-lane",
			AgentID:      "agent-lane",
			SystemPrompt: "Test",
		})

		lane := ag.Lane()
		require.NotNil(t, lane)

		id1 := lane.Submit("First message")
		id2 := lane.Submit("Second message")
		id3 := lane.Submit("Third message")

		assert.NotEmpty(t, id1)
		assert.NotEmpty(t, id2)
		assert.NotEmpty(t, id3)

		status := lane.Status()
		assert.Equal(t, 3, status.QueueDepth)

		drained := lane.DrainPending()
		assert.Len(t, drained, 3)

		status = lane.Status()
		assert.Equal(t, 0, status.QueueDepth)
	})

	t.Run("lane steer cancels pending messages", func(t *testing.T) {
		ag := agent.NewAgent(agent.AgentOptions{
			ID:           "agent-steer",
			UserID:       "user-steer",
			AgentID:      "agent-steer",
			SystemPrompt: "Test",
		})

		lane := ag.Lane()
		require.NotNil(t, lane)

		lane.Submit("Message 1")
		lane.Submit("Message 2")

		status := lane.Status()
		assert.Equal(t, 2, status.QueueDepth)

		cancelled := lane.Steer()
		assert.Equal(t, 2, cancelled)

		status = lane.Status()
		assert.Equal(t, 0, status.QueueDepth)
	})
}

func TestIntegrationWithPostgresStore(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test with Postgres in short mode")
	}

	pool := setupAgentTestDB(t)
	if pool == nil {
		return
	}

	ctx := context.Background()
	store := storage.NewPostgresAgentStore(pool)
	userID := "test-user-integration"
	createTestUser(t, ctx, pool, userID)

	t.Run("end-to-end agent creation and retrieval", func(t *testing.T) {
		agentConfig := &storage.AgentConfig{
			AgentID:     "agent-e2e",
			UserID:      userID,
			Name:        "E2E Test Agent",
			SoulMd:      "You are a test agent",
			AgentsMd:    "Test context",
			UserMd:      "User context",
			IdentityMd:  "Identity info",
			ToolsMd:     "Available tools",
			HeartbeatMd: "Status info",
			Config:      map[string]any{"setting": "value"},
		}

		err := store.Upsert(ctx, agentConfig)
		require.NoError(t, err, "Should store agent config")

		retrieved, err := store.Get(ctx, userID, "agent-e2e")
		require.NoError(t, err, "Should retrieve agent config")
		assert.Equal(t, agentConfig.AgentID, retrieved.AgentID)
		assert.Equal(t, agentConfig.Name, retrieved.Name)
		assert.Equal(t, agentConfig.SoulMd, retrieved.SoulMd)

		ag := agent.NewAgent(agent.AgentOptions{
			ID:           "agent-e2e",
			UserID:       userID,
			AgentID:      "agent-e2e",
			SystemPrompt: retrieved.SoulMd,
		})

		require.NotNil(t, ag)
		assert.Equal(t, "agent-e2e", ag.State().AgentID)
	})

	t.Run("agent lifecycle with store", func(t *testing.T) {
		config := &storage.AgentConfig{
			AgentID: "agent-lifecycle",
			UserID:  userID,
			Name:    "Lifecycle Test",
			SoulMd:  "Test soul",
		}

		err := store.Upsert(ctx, config)
		require.NoError(t, err)

		retrieved, err := store.Get(ctx, userID, "agent-lifecycle")
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		ag := agent.NewAgent(agent.AgentOptions{
			ID:           "agent-lifecycle",
			UserID:       userID,
			AgentID:      "agent-lifecycle",
			SystemPrompt: retrieved.SoulMd,
		})

		lane := ag.Lane()
		lane.Submit("Test message")

		status := lane.Status()
		assert.Equal(t, 1, status.QueueDepth)

		lane.DrainPending()
		status = lane.Status()
		assert.Equal(t, 0, status.QueueDepth)

		err = store.Delete(ctx, userID, "agent-lifecycle")
		require.NoError(t, err)

		_, err = store.Get(ctx, userID, "agent-lifecycle")
		assert.Error(t, err)
	})
}
