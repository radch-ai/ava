package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockWSConn struct {
	mock.Mock
	writeErr error
	mu       sync.Mutex
	writes   []interface{}
}

func (m *MockWSConn) WriteJSON(v interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return m.writeErr
	}
	m.writes = append(m.writes, v)
	return nil
}

func (m *MockWSConn) SetWriteError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeErr = err
}

func (m *MockWSConn) GetWrites() []interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writes
}

type MockEinoAgent struct {
	mock.Mock
}

func (m *MockEinoAgent) Query(ctx context.Context, input string, opts ...interface{}) (agent.EinoRunResult, error) {
	args := m.Called(ctx, input, opts)
	return args.Get(0).(agent.EinoRunResult), args.Error(1)
}

type MockEinoRunner struct {
	mock.Mock
	events []agent.EinoEvent
	mu     sync.Mutex
}

func (m *MockEinoRunner) Query(ctx context.Context, input string, handler interface{}) (<-chan agent.EinoEvent, error) {
	m.mu.Lock()
	events := make([]agent.EinoEvent, len(m.events))
	copy(events, m.events)
	m.mu.Unlock()

	args := m.Called(ctx, input, handler)

	ch := make(chan agent.EinoEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)

	var err error
	if len(args) > 1 {
		err = args.Error(1)
	}
	return ch, err
}

func (m *MockEinoRunner) SetEvents(events []agent.EinoEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = events
}

type MockAgentManager struct {
	mock.Mock
	evicted bool
	mu      sync.Mutex
}

func (m *MockAgentManager) Evict(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evicted = true
	m.Called(agentID)
}

func (m *MockAgentManager) WasEvicted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.evicted
}

type MockHistorySaver struct {
	mock.Mock
	saved bool
	data  []agent.AgentMessage
	mu    sync.Mutex
}

func (m *MockHistorySaver) SaveHistory(ctx context.Context, sessionID string, history []agent.AgentMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = true
	m.data = history
	args := m.Called(ctx, sessionID, history)
	return args.Error(0)
}

func (m *MockHistorySaver) WasSaved() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved
}

func (m *MockHistorySaver) GetSavedData() []agent.AgentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data
}

type MockNotifier struct {
	mock.Mock
	notified bool
	mu       sync.Mutex
}

func (m *MockNotifier) Notify(userID, agentID, msgID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notified = true
	args := m.Called(userID, agentID, msgID, message)
	return args.Error(0)
}

func (m *MockNotifier) WasNotified() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.notified
}

type MockMem0Client struct {
	mock.Mock
}

func (m *MockMem0Client) GetMemories(ctx context.Context, userID string) ([]string, error) {
	args := m.Called(ctx, userID)
	return args.Get(0).([]string), args.Error(1)
}

type MockQuotaMiddleware struct {
	mock.Mock
}

func (m *MockQuotaMiddleware) BeforeLLM(ctx context.Context, session interface{}) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}

func (m *MockQuotaMiddleware) AfterLLM(ctx context.Context, session interface{}, promptTokens, completionTokens int) {
	m.Called(ctx, session, promptTokens, completionTokens)
}

func TestNewEinoAgent(t *testing.T) {
	t.Run("creates agent with all fields initialized", func(t *testing.T) {
		opts := agent.EinoAgentOptions{
			AgentID:      "agent-123",
			UserID:       "user-456",
			SystemPrompt: "You are a helpful assistant",
		}

		ag := agent.NewEinoAgent(opts)

		require.NotNil(t, ag)
		require.Equal(t, "agent-123", ag.GetAgentID())
		require.Equal(t, "user-456", ag.GetUserID())
		require.NotNil(t, ag.GetContext())
	})

	t.Run("creates agent with laneQueue buffered channel", func(t *testing.T) {
		opts := agent.EinoAgentOptions{
			AgentID: "agent-queue",
			UserID:  "user-1",
		}

		ag := agent.NewEinoAgent(opts)
		require.NotNil(t, ag)
	})
}

func TestEinoAgent_Submit(t *testing.T) {
	t.Run("adds task to laneQueue and processes it", func(t *testing.T) {
		mockRunner := &MockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Hello!"},
			{Type: agent.EinoEventTypeEnd},
		})

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-submit",
			UserID:      "user-1",
			Runner:      mockRunner,
			IdleTimeout: 1 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &MockWSConn{}

		mockRunner.On("Query", mock.Anything, "Hello", mock.Anything).Return(nil)

		ag.Submit(agent.Task{
			Message: "Hello",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(50 * time.Millisecond)

		mockRunner.AssertExpectations(t)
	})

	t.Run("lazy starts goroutine on first submit", func(t *testing.T) {
		opts := agent.EinoAgentOptions{
			AgentID:     "agent-lazy",
			UserID:      "user-1",
			IdleTimeout: 1 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		require.False(t, ag.IsGoroutineStarted())

		mockConn := &MockWSConn{}
		ag.Submit(agent.Task{
			Message: "Hello",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(10 * time.Millisecond)
		require.True(t, ag.IsGoroutineStarted())
	})
}

func TestEinoAgent_RunLoop(t *testing.T) {
	t.Run("processes task from laneQueue", func(t *testing.T) {
		mockRunner := &MockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Hello!"},
			{Type: agent.EinoEventTypeEnd},
		})

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-runloop",
			UserID:      "user-1",
			Runner:      mockRunner,
			IdleTimeout: 100 * time.Millisecond,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &MockWSConn{}

		mockRunner.On("Query", mock.Anything, "Hello", mock.Anything).Return(nil)

		ag.Submit(agent.Task{
			Message: "Hello",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(50 * time.Millisecond)
		mockRunner.AssertExpectations(t)
	})

	t.Run("evicts on idle timeout", func(t *testing.T) {
		mockManager := &MockAgentManager{}
		mockManager.On("Evict", "agent-idle").Return()

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-idle",
			UserID:      "user-1",
			Manager:     mockManager,
			IdleTimeout: 50 * time.Millisecond,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &MockWSConn{}

		ag.Submit(agent.Task{
			Message: "trigger",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(100 * time.Millisecond)
		require.True(t, mockManager.WasEvicted())
		mockManager.AssertExpectations(t)
	})

	t.Run("stops on context cancellation", func(t *testing.T) {
		opts := agent.EinoAgentOptions{
			AgentID:     "agent-ctx",
			UserID:      "user-1",
			IdleTimeout: 5 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &MockWSConn{}

		ag.Submit(agent.Task{
			Message: "start",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(10 * time.Millisecond)
		ag.Cancel()
		time.Sleep(50 * time.Millisecond)
	})
}

func TestEinoAgent_RunTurn(t *testing.T) {
	t.Run("streams to connection when alive", func(t *testing.T) {
		mockRunner := &MockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response"},
			{Type: agent.EinoEventTypeEnd},
		})

		mockSaver := &MockHistorySaver{}
		mockSaver.On("SaveHistory", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-stream",
			UserID:       "user-1",
			Runner:       mockRunner,
			HistorySaver: mockSaver,
			IdleTimeout:  5 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &MockWSConn{}

		mockRunner.On("Query", mock.Anything, "Hello", mock.Anything).Return(nil)

		ag.Submit(agent.Task{
			Message: "Hello",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(100 * time.Millisecond)

		writes := mockConn.GetWrites()
		require.Greater(t, len(writes), 0)
		mockRunner.AssertExpectations(t)
	})

	t.Run("buffers when connection write fails", func(t *testing.T) {
		mockRunner := &MockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response"},
			{Type: agent.EinoEventTypeEnd},
		})

		mockSaver := &MockHistorySaver{}
		mockSaver.On("SaveHistory", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-buffer",
			UserID:       "user-1",
			Runner:       mockRunner,
			HistorySaver: mockSaver,
			IdleTimeout:  5 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &MockWSConn{}
		mockConn.SetWriteError(errors.New("connection closed"))

		mockRunner.On("Query", mock.Anything, "Hello", mock.Anything).Return(nil)

		ag.Submit(agent.Task{
			Message: "Hello",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(100 * time.Millisecond)

		require.True(t, mockSaver.WasSaved())
		mockRunner.AssertExpectations(t)
	})

	t.Run("sends notification when connection is dead", func(t *testing.T) {
		mockRunner := &MockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Response"},
			{Type: agent.EinoEventTypeEnd},
		})

		mockSaver := &MockHistorySaver{}
		mockSaver.On("SaveHistory", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		mockNotifier := &MockNotifier{}
		mockNotifier.On("Notify", "user-1", "agent-notify", "msg-1", mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-notify",
			UserID:       "user-1",
			Runner:       mockRunner,
			HistorySaver: mockSaver,
			Notifier:     mockNotifier,
			IdleTimeout:  5 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &MockWSConn{}
		mockConn.SetWriteError(errors.New("connection dead"))

		mockRunner.On("Query", mock.Anything, "Hello", mock.Anything).Return(nil)

		ag.Submit(agent.Task{
			Message: "Hello",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(100 * time.Millisecond)

		require.True(t, mockNotifier.WasNotified())
		mockRunner.AssertExpectations(t)
		mockNotifier.AssertExpectations(t)
	})
}

func TestEinoAgent_DrainBufferedMessages(t *testing.T) {
	t.Run("drains multiple buffered messages", func(t *testing.T) {
		mockRunner := &MockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeText, Content: "Combined response"},
			{Type: agent.EinoEventTypeEnd},
		})

		mockSaver := &MockHistorySaver{}
		mockSaver.On("SaveHistory", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		opts := agent.EinoAgentOptions{
			AgentID:      "agent-drain",
			UserID:       "user-1",
			Runner:       mockRunner,
			HistorySaver: mockSaver,
			IdleTimeout:  5 * time.Second,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &MockWSConn{}

		mockRunner.On("Query", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		ag.Submit(agent.Task{Message: "Msg1", Conn: mockConn, MsgID: "msg-1"})
		ag.Submit(agent.Task{Message: "Msg2", Conn: mockConn, MsgID: "msg-2"})
		ag.Submit(agent.Task{Message: "Msg3", Conn: mockConn, MsgID: "msg-3"})

		time.Sleep(100 * time.Millisecond)

		mockRunner.AssertExpectations(t)
	})
}

func TestEinoAgent_IdleEviction(t *testing.T) {
	t.Run("evicts agent after idle timeout", func(t *testing.T) {
		mockManager := &MockAgentManager{}
		mockManager.On("Evict", "agent-evict").Return()

		mockRunner := &MockEinoRunner{}
		mockRunner.SetEvents([]agent.EinoEvent{
			{Type: agent.EinoEventTypeEnd},
		})

		opts := agent.EinoAgentOptions{
			AgentID:     "agent-evict",
			UserID:      "user-1",
			Manager:     mockManager,
			Runner:      mockRunner,
			IdleTimeout: 50 * time.Millisecond,
		}

		ag := agent.NewEinoAgent(opts)
		mockConn := &MockWSConn{}

		mockRunner.On("Query", mock.Anything, "trigger", mock.Anything).Return(nil)

		ag.Submit(agent.Task{
			Message: "trigger",
			Conn:    mockConn,
			MsgID:   "msg-1",
		})

		time.Sleep(150 * time.Millisecond)

		require.True(t, mockManager.WasEvicted())
		mockManager.AssertExpectations(t)
	})
}

func TestEinoAgent_BuildSystemPrompt(t *testing.T) {
	t.Run("builds system prompt without memories", func(t *testing.T) {
		opts := agent.EinoAgentOptions{
			AgentID:      "agent-prompt",
			UserID:       "user-1",
			SystemPrompt: "You are a helpful assistant.",
		}

		ag := agent.NewEinoAgent(opts)
		prompt := ag.BuildSystemPrompt(nil)
		require.Equal(t, "You are a helpful assistant.", prompt)
	})

	t.Run("builds system prompt with memories", func(t *testing.T) {
		opts := agent.EinoAgentOptions{
			AgentID:      "agent-prompt",
			UserID:       "user-1",
			SystemPrompt: "You are a helpful assistant.",
		}

		ag := agent.NewEinoAgent(opts)
		memories := []string{"User likes pizza", "User lives in NYC"}
		prompt := ag.BuildSystemPrompt(memories)
		require.Contains(t, prompt, "You are a helpful assistant.")
		require.Contains(t, prompt, "Relevant memories:")
		require.Contains(t, prompt, "User likes pizza")
		require.Contains(t, prompt, "User lives in NYC")
	})
}
