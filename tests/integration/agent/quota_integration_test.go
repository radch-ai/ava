//go:build integration

package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/nevaria-ai/gateway/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockToolCallingChatModel struct {
	mock.Mock
	generateCalls int
	streamCalls   int
	mu            sync.Mutex
}

func (m *MockToolCallingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	m.generateCalls++
	m.mu.Unlock()

	args := m.Called(ctx, input, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*schema.Message), args.Error(1)
}

func (m *MockToolCallingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	m.mu.Lock()
	m.streamCalls++
	m.mu.Unlock()

	args := m.Called(ctx, input, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*schema.StreamReader[*schema.Message]), args.Error(1)
}

func (m *MockToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	args := m.Called(tools)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(model.ToolCallingChatModel), args.Error(1)
}

func (m *MockToolCallingChatModel) GetGenerateCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generateCalls
}

func (m *MockToolCallingChatModel) GetStreamCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streamCalls
}

type MockQuotaMiddleware struct {
	mock.Mock
	beforeCalls int
	afterCalls  int
	mu          sync.Mutex
}

func (m *MockQuotaMiddleware) BeforeLLM(ctx context.Context, session *llm.QuotaSession) error {
	m.mu.Lock()
	m.beforeCalls++
	m.mu.Unlock()
	args := m.Called(ctx, session)
	return args.Error(0)
}

func (m *MockQuotaMiddleware) AfterLLM(ctx context.Context, session *llm.QuotaSession, promptTokens, completionTokens int) {
	m.mu.Lock()
	m.afterCalls++
	m.mu.Unlock()
	m.Called(ctx, session, promptTokens, completionTokens)
}

func (m *MockQuotaMiddleware) GetBeforeCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.beforeCalls
}

func (m *MockQuotaMiddleware) GetAfterCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.afterCalls
}

func TestQuotaChatModel_Integration_FullFlow(t *testing.T) {
	t.Run("successful_generate_with_quota_tracking", func(t *testing.T) {
		ctx := context.Background()

		mockModel := new(MockToolCallingChatModel)
		mockQuota := new(MockQuotaMiddleware)
		session := &llm.QuotaSession{UserID: "user-123", AgentID: "agent-456"}

		input := []*schema.Message{{Role: schema.User, Content: "Hello"}}
		expectedResponse := &schema.Message{
			Role:    schema.Assistant,
			Content: "Hi there!",
			ResponseMeta: &schema.ResponseMeta{
				Usage: &schema.TokenUsage{
					PromptTokens:     10,
					CompletionTokens: 5,
					TotalTokens:      15,
				},
			},
		}

		mockQuota.On("BeforeLLM", ctx, session).Return(nil)
		mockModel.On("Generate", ctx, input, mock.Anything).Return(expectedResponse, nil)
		mockQuota.On("AfterLLM", ctx, session, 10, 5)

		quotaModel := llm.NewQuotaChatModel(mockModel, mockQuota, session)
		result, err := quotaModel.Generate(ctx, input)

		require.NoError(t, err)
		assert.Equal(t, expectedResponse, result)
		assert.Equal(t, 1, mockQuota.GetBeforeCalls())
		assert.Equal(t, 1, mockQuota.GetAfterCalls())
		assert.Equal(t, 1, mockModel.GetGenerateCalls())

		mockQuota.AssertExpectations(t)
		mockModel.AssertExpectations(t)
	})

	t.Run("quota_exhausted_before_generate", func(t *testing.T) {
		ctx := context.Background()

		mockModel := new(MockToolCallingChatModel)
		mockQuota := new(MockQuotaMiddleware)
		session := &llm.QuotaSession{UserID: "user-123", AgentID: "agent-456"}

		input := []*schema.Message{{Role: schema.User, Content: "Hello"}}
		quotaErr := errors.New("quota exhausted")

		mockQuota.On("BeforeLLM", ctx, session).Return(quotaErr)

		quotaModel := llm.NewQuotaChatModel(mockModel, mockQuota, session)
		result, err := quotaModel.Generate(ctx, input)

		require.Error(t, err)
		assert.ErrorIs(t, err, quotaErr)
		assert.Nil(t, result)
		assert.Equal(t, 1, mockQuota.GetBeforeCalls())
		assert.Equal(t, 0, mockQuota.GetAfterCalls())
		assert.Equal(t, 0, mockModel.GetGenerateCalls())

		mockQuota.AssertExpectations(t)
	})

	t.Run("concurrent_generate_calls", func(t *testing.T) {
		ctx := context.Background()

		mockModel := new(MockToolCallingChatModel)
		mockQuota := new(MockQuotaMiddleware)
		session := &llm.QuotaSession{UserID: "user-123", AgentID: "agent-456"}

		input := []*schema.Message{{Role: schema.User, Content: "Hello"}}
		expectedResponse := &schema.Message{
			Role:    schema.Assistant,
			Content: "Response",
			ResponseMeta: &schema.ResponseMeta{
				Usage: &schema.TokenUsage{
					PromptTokens:     10,
					CompletionTokens: 5,
				},
			},
		}

		mockQuota.On("BeforeLLM", ctx, session).Return(nil)
		mockModel.On("Generate", ctx, input, mock.Anything).Return(expectedResponse, nil)
		mockQuota.On("AfterLLM", ctx, session, 10, 5)

		quotaModel := llm.NewQuotaChatModel(mockModel, mockQuota, session)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := quotaModel.Generate(ctx, input)
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}()
		}
		wg.Wait()

		assert.Equal(t, 10, mockQuota.GetBeforeCalls())
		assert.Equal(t, 10, mockQuota.GetAfterCalls())
		assert.Equal(t, 10, mockModel.GetGenerateCalls())
	})

	t.Run("with_tools_preserves_quota_session", func(t *testing.T) {
		mockModel := new(MockToolCallingChatModel)
		mockQuota := new(MockQuotaMiddleware)
		session := &llm.QuotaSession{UserID: "user-123", AgentID: "agent-456"}

		tools := []*schema.ToolInfo{{Name: "test_tool"}}
		wrappedModel := new(MockToolCallingChatModel)

		mockModel.On("WithTools", tools).Return(wrappedModel, nil)

		quotaModel := llm.NewQuotaChatModel(mockModel, mockQuota, session)
		result, err := quotaModel.WithTools(tools)

		require.NoError(t, err)
		require.NotNil(t, result)

		_, ok := result.(*llm.QuotaChatModel)
		require.True(t, ok, "result should be a QuotaChatModel")

		mockModel.AssertExpectations(t)
	})

	t.Run("generate_without_usage_still_calls_quota", func(t *testing.T) {
		ctx := context.Background()

		mockModel := new(MockToolCallingChatModel)
		mockQuota := new(MockQuotaMiddleware)
		session := &llm.QuotaSession{UserID: "user-123", AgentID: "agent-456"}

		input := []*schema.Message{{Role: schema.User, Content: "Hello"}}
		expectedResponse := &schema.Message{
			Role:         schema.Assistant,
			Content:      "No usage info",
			ResponseMeta: nil,
		}

		mockQuota.On("BeforeLLM", ctx, session).Return(nil)
		mockModel.On("Generate", ctx, input, mock.Anything).Return(expectedResponse, nil)

		quotaModel := llm.NewQuotaChatModel(mockModel, mockQuota, session)
		result, err := quotaModel.Generate(ctx, input)

		require.NoError(t, err)
		assert.Equal(t, expectedResponse, result)
		assert.Equal(t, 1, mockQuota.GetBeforeCalls())
		assert.Equal(t, 0, mockQuota.GetAfterCalls())

		mockQuota.AssertExpectations(t)
		mockModel.AssertExpectations(t)
	})
}

func TestQuotaChatModel_Integration_ErrorScenarios(t *testing.T) {
	t.Run("inner_model_error_does_not_call_after", func(t *testing.T) {
		ctx := context.Background()

		mockModel := new(MockToolCallingChatModel)
		mockQuota := new(MockQuotaMiddleware)
		session := &llm.QuotaSession{UserID: "user-123", AgentID: "agent-456"}

		input := []*schema.Message{{Role: schema.User, Content: "Hello"}}
		modelErr := errors.New("model error")

		mockQuota.On("BeforeLLM", ctx, session).Return(nil)
		mockModel.On("Generate", ctx, input, mock.Anything).Return(nil, modelErr)

		quotaModel := llm.NewQuotaChatModel(mockModel, mockQuota, session)
		result, err := quotaModel.Generate(ctx, input)

		require.Error(t, err)
		assert.ErrorIs(t, err, modelErr)
		assert.Nil(t, result)
		assert.Equal(t, 1, mockQuota.GetBeforeCalls())
		assert.Equal(t, 0, mockQuota.GetAfterCalls())

		mockQuota.AssertExpectations(t)
		mockModel.AssertExpectations(t)
	})

	t.Run("context_cancelled_before_generate", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mockModel := new(MockToolCallingChatModel)
		mockQuota := new(MockQuotaMiddleware)
		session := &llm.QuotaSession{UserID: "user-123", AgentID: "agent-456"}

		input := []*schema.Message{{Role: schema.User, Content: "Hello"}}

		mockQuota.On("BeforeLLM", mock.Anything, session).Return(nil)
		mockModel.On("Generate", mock.Anything, input, mock.Anything).Return(nil, context.Canceled)

		quotaModel := llm.NewQuotaChatModel(mockModel, mockQuota, session)
		result, err := quotaModel.Generate(ctx, input)

		require.Error(t, err)
		assert.Nil(t, result)
	})
}
