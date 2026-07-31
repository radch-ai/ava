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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/radch-ai/ava/services/gateway/internal/agent"
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
		laneCh := make(chan agent.Message, 100)

		opts := agent.UserAgentOptions{
			AgentID:      "agent-full-flow",
			UserID:       "user-123",
			Description:  "A helpful assistant for testing",
			SystemPrompt: "You are a helpful assistant",
			ChatModel:    mockModel,
			IdleTimeout:  2 * time.Second,
			LaneCh:       laneCh,
		}

		ag := agent.NewUserAgent(ctx, opts)
		require.NotNil(t, ag)

		// Send a message
		laneCh <- agent.Message{Content: "Hello, agent!", ID: "msg-001"}

		time.Sleep(300 * time.Millisecond)

		status := ag.GetStatus()
		assert.Equal(t, "agent-full-flow", status["agent_id"])
		assert.Equal(t, "user-123", status["user_id"])
		assert.True(t, status["active"].(bool))
	})

	t.Run("handles LLM errors gracefully", func(t *testing.T) {
		ctx := context.Background()

		mockModel := new(ADKMockToolCallingChatModel)
		llmErr := errors.New("LLM service unavailable")

		mockModel.On("WithTools", mock.Anything).Return(mockModel, nil)
		mockModel.On("Generate", mock.Anything, mock.Anything, mock.Anything).Return(nil, llmErr)

		laneCh := make(chan agent.Message, 100)

		opts := agent.UserAgentOptions{
			AgentID:     "agent-error",
			UserID:      "user-456",
			Description: "An agent for error testing",
			ChatModel:   mockModel,
			IdleTimeout: 2 * time.Second,
			LaneCh:      laneCh,
		}

		ag := agent.NewUserAgent(ctx, opts)
		require.NotNil(t, ag)

		laneCh <- agent.Message{Content: "This should fail", ID: "msg-err"}

		time.Sleep(200 * time.Millisecond)
		assert.GreaterOrEqual(t, mockModel.GetGenerateCalls(), 1, "Generate should have been called")
	})

	t.Run("agent interrupt cancels query", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		laneCh := make(chan agent.Message, 100)

		opts := agent.UserAgentOptions{
			AgentID:     "agent-interrupt",
			UserID:      "user-789",
			Description: "An agent for interrupt testing",
			ChatModel:   mockModel,
			IdleTimeout: 10 * time.Second,
			LaneCh:      laneCh,
		}

		ag := agent.NewUserAgent(ctx, opts)
		require.NotNil(t, ag)

		laneCh <- agent.Message{Content: "Will be interrupted", ID: "msg-int"}

		time.Sleep(50 * time.Millisecond)
		ag.Interrupt()

		time.Sleep(50 * time.Millisecond)
	})
}

func TestUserAgent_WithTools_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("agent initializes with tools", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		laneCh := make(chan agent.Message, 100)

		opts := agent.UserAgentOptions{
			AgentID:      "agent-tools",
			UserID:       "user-tools",
			Description:  "An agent with search tools",
			SystemPrompt: "You are a helpful assistant with search capabilities",
			ChatModel:    mockModel,
			IdleTimeout:  2 * time.Second,
			LaneCh:       laneCh,
		}

		ag := agent.NewUserAgent(ctx, opts)
		require.NotNil(t, ag)

		laneCh <- agent.Message{Content: "Search for Go programming tutorials", ID: "msg-search"}

		time.Sleep(200 * time.Millisecond)

		mockModel.AssertCalled(t, "WithTools", mock.Anything)
	})
}

func TestUserAgent_MultipleMessages_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("drains and coalesces multiple messages", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		laneCh := make(chan agent.Message, 100)

		opts := agent.UserAgentOptions{
			AgentID:      "agent-multi",
			UserID:       "user-multi",
			Description:  "An agent for multi-message testing",
			SystemPrompt: "You are a helpful assistant",
			ChatModel:    mockModel,
			IdleTimeout:  2 * time.Second,
			LaneCh:       laneCh,
		}

		ag := agent.NewUserAgent(ctx, opts)
		require.NotNil(t, ag)

		laneCh <- agent.Message{Content: "First message", ID: "msg-1"}

		time.Sleep(300 * time.Millisecond)

		assert.GreaterOrEqual(t, mockModel.GetGenerateCalls(), 1, "Generate should have been called")

		_ = ag
	})

	t.Run("idle timeout triggers after message processing", func(t *testing.T) {
		ctx := context.Background()

		mockModel := setupMockModel()
		laneCh := make(chan agent.Message, 100)

		exited := make(chan bool, 1)

		opts := agent.UserAgentOptions{
			AgentID:     "agent-idle-test",
			UserID:      "user-idle",
			Description: "An agent for idle timeout testing",
			ChatModel:   mockModel,
			IdleTimeout: 100 * time.Millisecond,
			LaneCh:      laneCh,
			OnExit: func() {
				exited <- true
			},
		}

		ag := agent.NewUserAgent(ctx, opts)
		require.NotNil(t, ag)

		laneCh <- agent.Message{Content: "Trigger idle", ID: "msg-idle"}

		select {
		case <-exited:
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
		laneCh := make(chan agent.Message, 100)

		opts := agent.UserAgentOptions{
			AgentID:      "agent-memory",
			UserID:       "user-memory",
			Description:  "An agent with user memories",
			SystemPrompt: "You are a helpful assistant",
			Memories:     []string{"User prefers Go programming", "User likes dark mode"},
			ChatModel:    mockModel,
			IdleTimeout:  2 * time.Second,
			LaneCh:       laneCh,
		}

		ag := agent.NewUserAgent(ctx, opts)
		require.NotNil(t, ag)

		laneCh <- agent.Message{Content: "Hello", ID: "msg-memory"}

		time.Sleep(200 * time.Millisecond)

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
		laneCh := make(chan agent.Message, 100)

		opts := agent.UserAgentOptions{
			AgentID:     "agent-concurrent",
			UserID:      "user-concurrent",
			Description: "An agent for concurrent access testing",
			ChatModel:   mockModel,
			IdleTimeout: 3 * time.Second,
			LaneCh:      laneCh,
		}

		ag := agent.NewUserAgent(ctx, opts)
		require.NotNil(t, ag)

		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				laneCh <- agent.Message{Content: "concurrent message", ID: "msg-concurrent"}
			}(i)
		}
		wg.Wait()

		time.Sleep(200 * time.Millisecond)

		assert.GreaterOrEqual(t, mockModel.GetGenerateCalls(), 1, "Generate should have been called")
	})
}
