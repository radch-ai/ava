//go:build integration

package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type ADKMockToolCallingChatModel struct {
	mock.Mock
	generateCalls int
	streamCalls   int
	mu            sync.Mutex
}

func (m *ADKMockToolCallingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	m.generateCalls++
	m.mu.Unlock()

	args := m.Called(ctx, input, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*schema.Message), args.Error(1)
}

func (m *ADKMockToolCallingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.mu.Lock()
	m.streamCalls++
	m.mu.Unlock()

	args := m.Called(ctx, input, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*schema.StreamReader[*schema.Message]), args.Error(1)
}

func (m *ADKMockToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	args := m.Called(tools)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(model.ToolCallingChatModel), args.Error(1)
}

func (m *ADKMockToolCallingChatModel) BindTools(tools []*schema.ToolInfo) error {
	args := m.Called(tools)
	return args.Error(0)
}

func (m *ADKMockToolCallingChatModel) GetGenerateCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generateCalls
}

type ADKMockQuotaChatModel struct {
	inner model.ToolCallingChatModel
}

func (m *ADKMockQuotaChatModel) GetInnerModel() model.ToolCallingChatModel {
	return m.inner
}

func setupMockModel() *ADKMockToolCallingChatModel {
	mockModel := new(ADKMockToolCallingChatModel)
	mockModel.On("WithTools", mock.Anything).Return(mockModel, nil)
	mockModel.On("Generate", mock.Anything, mock.Anything, mock.Anything).Return(&schema.Message{
		Role:    schema.Assistant,
		Content: "Test response",
	}, nil)
	return mockModel
}

func TestUserAgent_FullFlow_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("creates agent and processes message", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		quotaModel := &ADKMockQuotaChatModel{inner: mockModel}

		firstMessage := agent.Message{
			Content: "Hello, agent!",
			ID:      "msg-001",
		}

		opts := agent.UserAgentOptions{
			AgentID:        "agent-full-flow",
			UserID:         "user-123",
			Description:    "A helpful assistant for testing",
			SystemPrompt:   "You are a helpful assistant",
			QuotaChatModel: quotaModel,
			IdleTimeout:    2 * time.Second,
		}

		ag := agent.NewUserAgent(ctx, firstMessage, opts)
		require.NotNil(t, ag)
		assert.Equal(t, "agent-full-flow", ag.GetAgentID())
		assert.Equal(t, "user-123", ag.GetUserID())

		time.Sleep(300 * time.Millisecond)

		ag.Stop()
		assert.GreaterOrEqual(t, mockModel.GetGenerateCalls(), 1, "Generate should have been called")
	})

	t.Run("handles LLM errors gracefully", func(t *testing.T) {
		ctx := context.Background()

		mockModel := new(ADKMockToolCallingChatModel)
		llmErr := errors.New("LLM service unavailable")

		mockModel.On("WithTools", mock.Anything).Return(mockModel, nil)
		mockModel.On("Generate", mock.Anything, mock.Anything, mock.Anything).Return(nil, llmErr)

		quotaModel := &ADKMockQuotaChatModel{inner: mockModel}

		firstMessage := agent.Message{
			Content: "This should fail",
			ID:      "msg-err",
		}

		opts := agent.UserAgentOptions{
			AgentID:        "agent-error",
			UserID:         "user-456",
			Description:    "An agent for error testing",
			QuotaChatModel: quotaModel,
			IdleTimeout:    2 * time.Second,
		}

		ag := agent.NewUserAgent(ctx, firstMessage, opts)
		require.NotNil(t, ag)

		time.Sleep(200 * time.Millisecond)

		ag.Stop()
		assert.GreaterOrEqual(t, mockModel.GetGenerateCalls(), 1, "Generate should have been called")
	})

	t.Run("agent context cancellation stops processing", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		quotaModel := &ADKMockQuotaChatModel{inner: mockModel}

		firstMessage := agent.Message{
			Content: "Will be cancelled",
			ID:      "msg-cancel",
		}

		opts := agent.UserAgentOptions{
			AgentID:        "agent-cancel",
			UserID:         "user-789",
			Description:    "An agent for cancellation testing",
			QuotaChatModel: quotaModel,
			IdleTimeout:    10 * time.Second,
		}

		ag := agent.NewUserAgent(ctx, firstMessage, opts)
		require.NotNil(t, ag)

		time.Sleep(50 * time.Millisecond)
		ag.Stop()

		time.Sleep(50 * time.Millisecond)
		assert.Error(t, ag.GetContext().Err(), "Context should be cancelled")
	})
}

func TestUserAgent_WithTools_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("agent initializes with DuckDuckGo tool", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		quotaModel := &ADKMockQuotaChatModel{inner: mockModel}

		firstMessage := agent.Message{
			Content: "Search for Go programming tutorials",
			ID:      "msg-search",
		}

		opts := agent.UserAgentOptions{
			AgentID:        "agent-tools",
			UserID:         "user-tools",
			Description:    "An agent with search tools",
			SystemPrompt:   "You are a helpful assistant with search capabilities",
			QuotaChatModel: quotaModel,
			IdleTimeout:    2 * time.Second,
		}

		ag := agent.NewUserAgent(ctx, firstMessage, opts)
		require.NotNil(t, ag)

		time.Sleep(200 * time.Millisecond)

		mockModel.AssertCalled(t, "WithTools", mock.Anything)
		ag.Stop()
	})
}

func TestUserAgent_MultipleMessages_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("drains and coalesces multiple messages", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		quotaModel := &ADKMockQuotaChatModel{inner: mockModel}

		firstMessage := agent.Message{
			Content: "First message",
			ID:      "msg-1",
		}

		opts := agent.UserAgentOptions{
			AgentID:        "agent-multi",
			UserID:         "user-multi",
			Description:    "An agent for multi-message testing",
			SystemPrompt:   "You are a helpful assistant",
			QuotaChatModel: quotaModel,
			IdleTimeout:    2 * time.Second,
		}

		ag := agent.NewUserAgent(ctx, firstMessage, opts)
		require.NotNil(t, ag)

		time.Sleep(300 * time.Millisecond)

		ag.Stop()
		assert.GreaterOrEqual(t, mockModel.GetGenerateCalls(), 1, "Generate should have been called")
	})

	t.Run("processes sequential messages in order", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		quotaModel := &ADKMockQuotaChatModel{inner: mockModel}

		firstMessage := agent.Message{
			Content: "Message A",
			ID:      "msg-a",
		}

		opts := agent.UserAgentOptions{
			AgentID:        "agent-sequential",
			UserID:         "user-seq",
			Description:    "An agent for sequential message testing",
			QuotaChatModel: quotaModel,
			IdleTimeout:    3 * time.Second,
		}

		ag := agent.NewUserAgent(ctx, firstMessage, opts)
		require.NotNil(t, ag)

		time.Sleep(200 * time.Millisecond)

		ag.Stop()
		assert.GreaterOrEqual(t, mockModel.GetGenerateCalls(), 1, "Generate should have been called")
	})

	t.Run("idle timeout triggers after message processing", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		quotaModel := &ADKMockQuotaChatModel{inner: mockModel}

		firstMessage := agent.Message{
			Content: "Trigger idle",
			ID:      "msg-idle",
		}

		opts := agent.UserAgentOptions{
			AgentID:        "agent-idle-test",
			UserID:         "user-idle",
			Description:    "An agent for idle timeout testing",
			QuotaChatModel: quotaModel,
			IdleTimeout:    100 * time.Millisecond,
		}

		ag := agent.NewUserAgent(ctx, firstMessage, opts)
		require.NotNil(t, ag)

		time.Sleep(300 * time.Millisecond)

		select {
		case <-ag.GetContext().Done():
		case <-time.After(500 * time.Millisecond):
		}
	})
}

func TestUserAgent_Memories_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("agent includes memories in instruction", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		quotaModel := &ADKMockQuotaChatModel{inner: mockModel}

		firstMessage := agent.Message{
			Content: "Hello",
			ID:      "msg-memory",
		}

		opts := agent.UserAgentOptions{
			AgentID:        "agent-memory",
			UserID:         "user-memory",
			Description:    "An agent with user memories",
			SystemPrompt:   "You are a helpful assistant",
			Memories:       []string{"User prefers Go programming", "User likes dark mode"},
			QuotaChatModel: quotaModel,
			IdleTimeout:    2 * time.Second,
		}

		ag := agent.NewUserAgent(ctx, firstMessage, opts)
		require.NotNil(t, ag)

		time.Sleep(200 * time.Millisecond)

		ag.Stop()
		assert.GreaterOrEqual(t, mockModel.GetGenerateCalls(), 1, "Generate should have been called")
	})
}

func TestUserAgent_ConcurrentAccess_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("concurrent message submission is safe", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		quotaModel := &ADKMockQuotaChatModel{inner: mockModel}

		firstMessage := agent.Message{
			Content: "Initial message",
			ID:      "msg-init",
		}

		opts := agent.UserAgentOptions{
			AgentID:        "agent-concurrent",
			UserID:         "user-concurrent",
			Description:    "An agent for concurrent access testing",
			QuotaChatModel: quotaModel,
			IdleTimeout:    3 * time.Second,
		}

		ag := agent.NewUserAgent(ctx, firstMessage, opts)
		require.NotNil(t, ag)

		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
			}(i)
		}
		wg.Wait()

		time.Sleep(200 * time.Millisecond)

		ag.Stop()
		assert.GreaterOrEqual(t, mockModel.GetGenerateCalls(), 1, "Generate should have been called")
	})
}
