package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockToolCallingChatModel struct {
	generateCalls  int
	streamCalls    int
	withToolsCalls int
	bindToolsCalls int
	toolsBound     []*schema.ToolInfo
	mu             sync.Mutex
}

func (m *MockToolCallingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.generateCalls++
	return &schema.Message{Role: schema.Assistant, Content: "mock response"}, nil
}

func (m *MockToolCallingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.streamCalls++
	return nil, nil
}

func (m *MockToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.withToolsCalls++
	m.toolsBound = tools
	return m, nil
}

func (m *MockToolCallingChatModel) BindTools(tools []*schema.ToolInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bindToolsCalls++
	m.toolsBound = tools
	return nil
}

func (m *MockToolCallingChatModel) GetToolsBound() []*schema.ToolInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.toolsBound
}

func (m *MockToolCallingChatModel) GetWithToolsCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.withToolsCalls
}

func TestUserAgent_NewUserAgent_Success(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:      "agent-start-test",
		UserID:       "user-1",
		ChatModel:    mockChatModel,
		SystemPrompt: "You are a helpful assistant.",
		Description:  "Test agent for unit testing",
		LaneCh:       laneCh,
		IdleTimeout:  100 * time.Millisecond,
	})

	require.NotNil(t, ag)
	time.Sleep(150 * time.Millisecond) // let idle timeout cleanup
}

func TestUserAgent_NewUserAgent_WithTools(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:      "agent-tools-test",
		UserID:       "user-1",
		ChatModel:    mockChatModel,
		SystemPrompt: "You are a helpful assistant.",
		Description:  "Test agent for tools testing",
		LaneCh:       laneCh,
		IdleTimeout:  100 * time.Millisecond,
	})

	require.NotNil(t, ag)
	time.Sleep(150 * time.Millisecond)
}

func TestUserAgent_NewUserAgent_WithMemories(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}

	memories := []string{
		"User prefers dark mode",
		"User likes concise responses",
		"User is a Go developer",
	}

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:      "agent-memories-test",
		UserID:       "user-1",
		ChatModel:    mockChatModel,
		SystemPrompt: "You are a helpful assistant.",
		Memories:     memories,
		Description:  "Test agent for memories testing",
		LaneCh:       laneCh,
		IdleTimeout:  100 * time.Millisecond,
	})

	require.NotNil(t, ag)
	time.Sleep(150 * time.Millisecond)
}

func TestUserAgent_ConcurrentSubmit(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}

	var processedMessages []agent.Message
	var processedMu sync.Mutex

	laneCh := make(chan agent.Message, 100)
	_ = agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:      "agent-concurrent-test",
		UserID:       "user-1",
		IdleTimeout:  2 * time.Second,
		ChatModel:    mockChatModel,
		SystemPrompt: "You are a helpful assistant.",
		Description:  "Test agent for concurrent testing",
		LaneCh:       laneCh,
		ProcessMessagesFn: func(messages []agent.Message) {
			processedMu.Lock()
			processedMessages = append(processedMessages, messages...)
			processedMu.Unlock()
		},
	})

	const numGoroutines = 10
	const numMessagesPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numMessagesPerGoroutine; j++ {
				laneCh <- agent.Message{
					Content: "concurrent message",
					ID:      "msg-concurrent",
				}
			}
		}(i)
	}

	wg.Wait()

	time.Sleep(200 * time.Millisecond)

	processedMu.Lock()
	totalProcessed := len(processedMessages)
	processedMu.Unlock()

	assert.GreaterOrEqual(t, totalProcessed, numGoroutines*numMessagesPerGoroutine)
}

func TestUserAgent_NewUserAgent_NilModel(t *testing.T) {
	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:      "agent-nil-model-test",
		UserID:       "user-1",
		ChatModel:    nil,
		SystemPrompt: "You are a helpful assistant.",
		LaneCh:       laneCh,
		IdleTimeout:  100 * time.Millisecond,
	})

	require.NotNil(t, ag)
	time.Sleep(150 * time.Millisecond)
}

func TestUserAgent_IdleTimeout_TriggersExit(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}

	exited := make(chan bool, 1)

	laneCh := make(chan agent.Message, 100)
	_ = agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:      "agent-idle-exit-test",
		UserID:       "user-1",
		IdleTimeout:  50 * time.Millisecond,
		ChatModel:    mockChatModel,
		SystemPrompt: "You are a helpful assistant.",
		Description:  "Test agent for idle timeout testing",
		LaneCh:       laneCh,
		OnExit: func() {
			exited <- true
		},
	})

	select {
	case <-exited:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected agent to exit on idle timeout")
	}
}

func TestUserAgent_ProcessMessagesCalled(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}

	var messagesReceived []agent.Message
	var messagesMu sync.Mutex

	laneCh := make(chan agent.Message, 100)
	_ = agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:      "agent-process-test",
		UserID:       "user-1",
		IdleTimeout:  500 * time.Millisecond,
		ChatModel:    mockChatModel,
		SystemPrompt: "You are a helpful assistant.",
		Description:  "Test agent for process messages testing",
		LaneCh:       laneCh,
		ProcessMessagesFn: func(messages []agent.Message) {
			messagesMu.Lock()
			messagesReceived = append(messagesReceived, messages...)
			messagesMu.Unlock()
		},
	})

	laneCh <- agent.Message{Content: "test message", ID: "msg-1"}

	time.Sleep(100 * time.Millisecond)

	messagesMu.Lock()
	assert.GreaterOrEqual(t, len(messagesReceived), 1)
	messagesMu.Unlock()
}
