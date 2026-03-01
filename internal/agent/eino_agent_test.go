package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type MockEinoRunner struct {
	events  chan EinoEvent
	err     error
	mu      sync.Mutex
	called  int
	queryCh chan string
}

func NewMockEinoRunner() *MockEinoRunner {
	return &MockEinoRunner{
		events:  make(chan EinoEvent, 10),
		queryCh: make(chan string, 10),
	}
}

func (m *MockEinoRunner) Query(ctx context.Context, input string, handler interface{}) (<-chan EinoEvent, error) {
	m.mu.Lock()
	m.called++
	m.mu.Unlock()

	if m.err != nil {
		return nil, m.err
	}

	m.queryCh <- input
	return m.events, nil
}

func (m *MockEinoRunner) SendTextEvent(content string) {
	m.events <- EinoEvent{Type: EinoEventTypeText, Content: content}
}

func (m *MockEinoRunner) SendToolEvent(name string, args map[string]any) {
	m.events <- EinoEvent{Type: EinoEventTypeTool, ToolName: name, ToolArgs: args}
}

func (m *MockEinoRunner) SendErrorEvent(err error) {
	m.events <- EinoEvent{Type: EinoEventTypeError, Error: err}
}

func (m *MockEinoRunner) Close() {
	close(m.events)
}

func (m *MockEinoRunner) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

type MockHistorySaver struct {
	history map[string][]AgentMessage
	mu      sync.Mutex
	err     error
}

func NewMockHistorySaver() *MockHistorySaver {
	return &MockHistorySaver{
		history: make(map[string][]AgentMessage),
	}
}

func (m *MockHistorySaver) SaveHistory(ctx context.Context, sessionID string, history []AgentMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.history[sessionID] = history
	return nil
}

func (m *MockHistorySaver) GetHistory(sessionID string) []AgentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.history[sessionID]
}

type MockNotifier struct {
	calls []struct {
		userID, agentID, msgID, message string
	}
	mu  sync.Mutex
	err error
}

func NewMockNotifier() *MockNotifier {
	return &MockNotifier{
		calls: make([]struct{ userID, agentID, msgID, message string }, 0),
	}
}

func (m *MockNotifier) Notify(userID, agentID, msgID, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.calls = append(m.calls, struct{ userID, agentID, msgID, message string }{userID, agentID, msgID, message})
	return nil
}

func (m *MockNotifier) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

type MockMem0Client struct {
	memories []string
	err      error
}

func (m *MockMem0Client) GetMemories(ctx context.Context, userID string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.memories, nil
}

func TestEinoAgent_NewEinoAgent_DefaultValues(t *testing.T) {
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID: "agent-1",
		UserID:  "user-1",
	})

	assert.Equal(t, "agent-1", agent.GetAgentID())
	assert.Equal(t, "user-1", agent.GetUserID())
	assert.Equal(t, defaultIdleTimeout, agent.idleTimeout)
	assert.NotNil(t, agent.GetContext())
}

func TestEinoAgent_NewEinoAgent_CustomIdleTimeout(t *testing.T) {
	customTimeout := 10 * time.Second
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID:     "agent-1",
		UserID:      "user-1",
		IdleTimeout: customTimeout,
	})

	assert.Equal(t, customTimeout, agent.idleTimeout)
}

func TestEinoAgent_GetQueueStats(t *testing.T) {
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID: "agent-1",
		UserID:  "user-1",
	})

	queued, stats := agent.GetQueueStats()
	assert.Equal(t, 0, queued)
	assert.Equal(t, 0, stats.Queued)
	assert.False(t, stats.Running)
}

func TestEinoAgent_Submit_StartsGoroutine(t *testing.T) {
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID: "agent-1",
		UserID:  "user-1",
	})

	assert.False(t, agent.IsGoroutineStarted())

	agent.Submit(Task{Message: "test"})

	assert.True(t, agent.IsGoroutineStarted())
}

func TestEinoAgent_SubmitToFullQueue_DropsMessage(t *testing.T) {
	blockCh := make(chan struct{})
	runner := NewMockEinoRunner()
	runner.err = nil

	agent := NewEinoAgent(EinoAgentOptions{
		AgentID: "agent-1",
		UserID:  "user-1",
		Runner:  runner,
	})

	for i := 0; i < defaultLaneQueueSize; i++ {
		agent.Submit(Task{Message: "fill"})
	}

	for i := 0; i < 10; i++ {
		agent.Submit(Task{Message: "overflow"})
	}

	agent.Cancel()
	close(blockCh)

	assert.True(t, agent.IsGoroutineStarted())
}

func TestEinoAgent_MultipleMessagesDrainedTogether(t *testing.T) {
	runner := NewMockEinoRunner()
	saver := NewMockHistorySaver()

	agent := NewEinoAgent(EinoAgentOptions{
		AgentID:      "agent-1",
		UserID:       "user-1",
		Runner:       runner,
		HistorySaver: saver,
	})

	agent.Submit(Task{Message: "message1"})
	agent.Submit(Task{Message: "message2"})
	agent.Submit(Task{Message: "message3"})

	select {
	case query := <-runner.queryCh:
		assert.Contains(t, query, "message1")
		assert.Contains(t, query, "message2")
		assert.Contains(t, query, "message3")
	case <-time.After(100 * time.Millisecond):
	}

	runner.SendTextEvent("response")
	runner.Close()

	agent.Cancel()
}

func TestEinoAgent_StopDuringProcessing(t *testing.T) {
	runner := NewMockEinoRunner()

	agent := NewEinoAgent(EinoAgentOptions{
		AgentID: "agent-1",
		UserID:  "user-1",
		Runner:  runner,
	})

	agent.Submit(Task{Message: "test"})

	<-runner.queryCh

	agent.Cancel()

	assert.True(t, agent.IsGoroutineStarted())
}

func TestEinoAgent_BuildSystemPrompt_WithMemories(t *testing.T) {
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID:      "agent-1",
		UserID:       "user-1",
		SystemPrompt: "You are a helpful assistant.",
	})

	memories := []string{"User likes coffee", "User prefers dark mode"}
	prompt := agent.BuildSystemPrompt(memories)

	assert.Contains(t, prompt, "You are a helpful assistant.")
	assert.Contains(t, prompt, "Relevant memories:")
	assert.Contains(t, prompt, "User likes coffee")
	assert.Contains(t, prompt, "User prefers dark mode")
}

func TestEinoAgent_BuildSystemPrompt_WithEmptyMem0(t *testing.T) {
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID:      "agent-1",
		UserID:       "user-1",
		SystemPrompt: "You are a helpful assistant.",
	})

	prompt := agent.BuildSystemPrompt([]string{})

	assert.Equal(t, "You are a helpful assistant.", prompt)
	assert.NotContains(t, prompt, "Relevant memories:")
}

func TestEinoAgent_BuildSystemPrompt_WithNilMemories(t *testing.T) {
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID:      "agent-1",
		UserID:       "user-1",
		SystemPrompt: "You are a helpful assistant.",
	})

	prompt := agent.BuildSystemPrompt(nil)

	assert.Equal(t, "You are a helpful assistant.", prompt)
}

func TestEinoAgent_BuildSystemPrompt_EmptySystemPrompt(t *testing.T) {
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID:      "agent-1",
		UserID:       "user-1",
		SystemPrompt: "",
	})

	memories := []string{"memory1"}
	prompt := agent.BuildSystemPrompt(memories)

	assert.Contains(t, prompt, "Relevant memories:")
	assert.Contains(t, prompt, "memory1")
}

func TestEinoAgent_Cancel_TerminatesRunLoop(t *testing.T) {
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID: "agent-1",
		UserID:  "user-1",
	})

	agent.Submit(Task{Message: "test"})

	time.Sleep(10 * time.Millisecond)

	agent.Cancel()

	ctx := agent.GetContext()
	select {
	case <-ctx.Done():
	default:
		t.Error("context should be cancelled")
	}
}

func TestEinoAgent_ConcurrentSubmit(t *testing.T) {
	runner := NewMockEinoRunner()
	agent := NewEinoAgent(EinoAgentOptions{
		AgentID: "agent-1",
		UserID:  "user-1",
		Runner:  runner,
	})

	numGoroutines := 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			agent.Submit(Task{Message: "message"})
		}(i)
	}

	wg.Wait()

	assert.True(t, agent.IsGoroutineStarted())

	agent.Cancel()
}

func TestEinoAgent_WithNilRunner(t *testing.T) {
	saver := NewMockHistorySaver()

	agent := NewEinoAgent(EinoAgentOptions{
		AgentID:      "agent-1",
		UserID:       "user-1",
		Runner:       nil,
		HistorySaver: saver,
	})

	agent.Submit(Task{Message: "test"})

	time.Sleep(50 * time.Millisecond)

	agent.Cancel()

	assert.True(t, agent.IsGoroutineStarted())
}
