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

func TestUserAgent_NewUserAgent_Success(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:        "agent-start-test",
		UserID:         "user-1",
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for unit testing",
		LaneCh:         laneCh,
	})

	require.NotNil(t, ag)
	ag.Stop()
}

func TestUserAgent_NewUserAgent_WithTools(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:        "agent-tools-test",
		UserID:         "user-1",
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for tools testing",
		LaneCh:         laneCh,
	})

	time.Sleep(50 * time.Millisecond)
	ag.Stop()
}

func TestUserAgent_NewUserAgent_WithMemories(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	memories := []string{
		"User prefers dark mode",
		"User likes concise responses",
		"User is a Go developer",
	}

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:        "agent-memories-test",
		UserID:         "user-1",
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Memories:       memories,
		Description:    "Test agent for memories testing",
		LaneCh:         laneCh,
	})

	ag.Stop()
}

func TestUserAgent_ConcurrentSubmit(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	var processedMessages []agent.Message
	var processedMu sync.Mutex

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:        "agent-concurrent-test",
		UserID:         "user-1",
		IdleTimeout:    2 * time.Second,
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for concurrent testing",
		LaneCh:         laneCh,
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

	ag.Stop()
}

func TestUserAgent_ConnWrapper_Integration(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	wrapper := wsconn.NewWrapper(nil)

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:        "agent-wsconn-test",
		UserID:         "user-1",
		Conn:           wrapper,
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for ConnWrapper testing",
		LaneCh:         laneCh,
	})

	conn := ag.GetConn()
	require.NotNil(t, conn)

	assert.Nil(t, conn.Conn())

	// Test UpdateConn
	ag.UpdateConn(nil)
	assert.Nil(t, conn.Conn())

	ag.Stop()
}

func TestUserAgent_NewUserAgent_NilQuotaModel(t *testing.T) {
	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:        "agent-nil-model-test",
		UserID:         "user-1",
		QuotaChatModel: nil,
		SystemPrompt:   "You are a helpful assistant.",
		LaneCh:         laneCh,
	})

	require.NotNil(t, ag)
	ag.Stop()
}

func TestUserAgent_Submit_AfterStop(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:        "agent-submit-stop-test",
		UserID:         "user-1",
		IdleTimeout:    100 * time.Millisecond,
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for submit after stop testing",
		LaneCh:         laneCh,
	})

	time.Sleep(50 * time.Millisecond)

	ag.Stop()

	// Send to lane after stop (will be ignored since agent is stopped)
	laneCh <- agent.Message{
		Content: "after stop",
		ID:      "msg-after-stop",
	}
}

func TestUserAgent_GetContext(t *testing.T) {
	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID: "agent-ctx-test",
		UserID:  "user-1",
		LaneCh:  laneCh,
	})

	ctx := ag.GetContext()
	require.NotNil(t, ctx)

	assert.Nil(t, ctx.Err())

	ag.Stop()

	time.Sleep(10 * time.Millisecond)

	assert.NotNil(t, ag.GetContext().Err())
}

func TestUserAgent_GetAgentID_GetUserID(t *testing.T) {
	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID: "agent-id-test",
		UserID:  "user-id-test",
		LaneCh:  laneCh,
	})

	assert.Equal(t, "agent-id-test", ag.GetAgentID())
	assert.Equal(t, "user-id-test", ag.GetUserID())
	ag.Stop()
}

func TestUserAgent_IdleTimeout_TriggersExit(t *testing.T) {
	mockChatModel := &MockToolCallingChatModel{}
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	exited := make(chan bool, 1)

	laneCh := make(chan agent.Message, 100)
	_ = agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:        "agent-idle-exit-test",
		UserID:         "user-1",
		IdleTimeout:    50 * time.Millisecond,
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for idle timeout testing",
		LaneCh:         laneCh,
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
	mockQuotaModel := &MockQuotaChatModel{}
	mockQuotaModel.SetInnerModel(mockChatModel)

	var messagesReceived []agent.Message
	var messagesMu sync.Mutex

	laneCh := make(chan agent.Message, 100)
	ag := agent.NewUserAgent(context.Background(), agent.UserAgentOptions{
		AgentID:        "agent-process-test",
		UserID:         "user-1",
		IdleTimeout:    500 * time.Millisecond,
		QuotaChatModel: mockQuotaModel,
		SystemPrompt:   "You are a helpful assistant.",
		Description:    "Test agent for process messages testing",
		LaneCh:         laneCh,
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

	ag.Stop()
}
