package agent_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/nevaria-ai/gateway/internal/wsconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockQuotaChatModel struct {
	inner    model.ToolCallingChatModel
	innerMux sync.RWMutex
}

func (m *MockQuotaChatModel) GetInnerModel() model.ToolCallingChatModel {
	m.innerMux.RLock()
	defer m.innerMux.RUnlock()
	return m.inner
}

func (m *MockQuotaChatModel) SetInnerModel(inner model.ToolCallingChatModel) {
	m.innerMux.Lock()
	defer m.innerMux.Unlock()
	m.inner = inner
}

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

func TestADKAgent_Start_Success(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID: "agent-start-test",
		UserID:  "user-1",
	})

	opts := agent.ADKAgentOptions{
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for unit testing",
	}

	err := ag.Start(opts)
	require.NoError(t, err)

	ag.Stop()
}

func TestADKAgent_Start_WithTools(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID: "agent-tools-test",
		UserID:  "user-1",
	})

	opts := agent.ADKAgentOptions{
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for tools testing",
	}

	err := ag.Start(opts)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	ag.Stop()
}

func TestADKAgent_Start_WithMemories(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	memories := []string{
		"User prefers dark mode",
		"User likes concise responses",
		"User is a Go developer",
	}

	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID: "agent-memories-test",
		UserID:  "user-1",
	})

	opts := agent.ADKAgentOptions{
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Memories:       memories,
		Description:    "Test agent for memories testing",
	}

	err := ag.Start(opts)
	require.NoError(t, err)

	ag.Stop()
}

func TestADKAgent_ConcurrentSubmit(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	var processedMessages []agent.Message
	var processedMu sync.Mutex

	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "initial"}, agent.ADKAgentOptions{
		AgentID:     "agent-concurrent-test",
		UserID:      "user-1",
		IdleTimeout: 2 * time.Second,
	})

	ag.SetProcessMessagesFn(func(messages []agent.Message) {
		processedMu.Lock()
		processedMessages = append(processedMessages, messages...)
		processedMu.Unlock()
	})

	opts := agent.ADKAgentOptions{
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for concurrent testing",
	}

	err := ag.Start(opts)
	require.NoError(t, err)

	const numGoroutines = 10
	const numMessagesPerGoroutine = 5

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < numMessagesPerGoroutine; j++ {
				ag.Submit(agent.Message{
					Content: "concurrent message",
					ID:      "msg-concurrent",
				})
			}
		}(i)
	}

	wg.Wait()

	time.Sleep(200 * time.Millisecond)

	processedMu.Lock()
	totalProcessed := len(processedMessages)
	processedMu.Unlock()

	assert.GreaterOrEqual(t, totalProcessed, numGoroutines*numMessagesPerGoroutine)

	ag.Stop()
}

func TestADKAgent_ConnWrapper_Integration(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	wrapper := wsconn.NewWrapper(nil)

	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID: "agent-wsconn-test",
		UserID:  "user-1",
		Conn:    wrapper,
	})

	conn := ag.GetConn()
	require.NotNil(t, conn)

	assert.Nil(t, conn.Conn())

	// Test UpdateConn
	ag.UpdateConn(nil)
	assert.Nil(t, conn.Conn())

	opts := agent.ADKAgentOptions{
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for ConnWrapper testing",
	}

	err := ag.Start(opts)
	require.NoError(t, err)

	ag.Stop()
}

func TestADKAgent_Start_NilQuotaModel(t *testing.T) {
	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID: "agent-nil-model-test",
		UserID:  "user-1",
	})

	opts := agent.ADKAgentOptions{
		QuotaChatModel: nil,
		SystemPrompt:   "You are a helpful assistant.",
	}

	defer func() {
		_ = recover()
	}()

	_ = ag.Start(opts)
}

func TestADKAgent_Submit_AfterStop(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID:     "agent-submit-stop-test",
		UserID:      "user-1",
		IdleTimeout: 100 * time.Millisecond,
	})

	opts := agent.ADKAgentOptions{
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for submit after stop testing",
	}

	err := ag.Start(opts)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	ag.Stop()

	ag.Submit(agent.Message{
		Content: "after stop",
		ID:      "msg-after-stop",
	})
}

func TestADKAgent_GetContext(t *testing.T) {
	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID: "agent-ctx-test",
		UserID:  "user-1",
	})

	ctx := ag.GetContext()
	require.NotNil(t, ctx)

	assert.Nil(t, ctx.Err())

	ag.Stop()

	time.Sleep(10 * time.Millisecond)

	assert.NotNil(t, ag.GetContext().Err())
}

func TestADKAgent_GetAgentID_GetUserID(t *testing.T) {
	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID: "agent-id-test",
		UserID:  "user-id-test",
	})

	assert.Equal(t, "agent-id-test", ag.GetAgentID())
	assert.Equal(t, "user-id-test", ag.GetUserID())
}

func TestADKAgent_MultipleStartCalls(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID:     "agent-multi-start-test",
		UserID:      "user-1",
		IdleTimeout: 100 * time.Millisecond,
	})

	opts := agent.ADKAgentOptions{
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for multiple start testing",
	}

	err := ag.Start(opts)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	ag.Stop()
}

func TestADKAgent_IdleTimeout_TriggersExit(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	exited := make(chan bool, 1)

	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "hello"}, agent.ADKAgentOptions{
		AgentID:     "agent-idle-exit-test",
		UserID:      "user-1",
		IdleTimeout: 50 * time.Millisecond,
	})

	ag.SetOnExit(func() {
		exited <- true
	})

	opts := agent.ADKAgentOptions{
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for idle timeout testing",
	}

	err := ag.Start(opts)
	require.NoError(t, err)

	select {
	case <-exited:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected agent to exit on idle timeout")
	}
}

func TestADKAgent_ProcessMessagesCalled(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	var messagesReceived []agent.Message
	var messagesMu sync.Mutex

	ag := agent.NewADKAgent(context.Background(), agent.Message{Content: "initial"}, agent.ADKAgentOptions{
		AgentID:     "agent-process-test",
		UserID:      "user-1",
		IdleTimeout: 500 * time.Millisecond,
	})

	ag.SetProcessMessagesFn(func(messages []agent.Message) {
		messagesMu.Lock()
		messagesReceived = append(messagesReceived, messages...)
		messagesMu.Unlock()
	})

	opts := agent.ADKAgentOptions{
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for process messages testing",
	}

	err := ag.Start(opts)
	require.NoError(t, err)

	ag.Submit(agent.Message{Content: "test message", ID: "msg-1"})

	time.Sleep(100 * time.Millisecond)

	messagesMu.Lock()
	assert.GreaterOrEqual(t, len(messagesReceived), 1)
	messagesMu.Unlock()

	ag.Stop()
}
