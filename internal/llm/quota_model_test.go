package llm

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockToolCallingChatModel struct {
	mock.Mock
}

func (m *MockToolCallingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	args := m.Called(ctx, input, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*schema.Message), args.Error(1)
}

func (m *MockToolCallingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
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

type MockQuotaMiddleware struct {
	mock.Mock
}

func (m *MockQuotaMiddleware) BeforeLLM(ctx context.Context, session *QuotaSession) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}

func (m *MockQuotaMiddleware) AfterLLM(ctx context.Context, session *QuotaSession, promptTokens, completionTokens int) {
	m.Called(ctx, session, promptTokens, completionTokens)
}

func TestQuotaChatModel_ImplementsInterface(t *testing.T) {
	var _ model.ToolCallingChatModel = (*QuotaChatModel)(nil)
}

func TestQuotaChatModel_Generate_CallsQuota(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	expectedResponse := &schema.Message{
		Role:    schema.Assistant,
		Content: "hi",
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

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.Generate(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, expectedResponse, result)
	mockQuota.AssertExpectations(t)
	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_Generate_QuotaExhausted(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	quotaErr := errors.New("quota exhausted")

	mockQuota.On("BeforeLLM", ctx, session).Return(quotaErr)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.Generate(ctx, input)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, quotaErr)
	mockModel.AssertNotCalled(t, "Generate")
	mockQuota.AssertExpectations(t)
}

func TestQuotaChatModel_Generate_InnerError(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	innerErr := errors.New("inner model error")

	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Generate", ctx, input, mock.Anything).Return(nil, innerErr)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.Generate(ctx, input)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, innerErr)
	mockQuota.AssertExpectations(t)
	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_Generate_NoUsage(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	expectedResponse := &schema.Message{
		Role:    schema.Assistant,
		Content: "hi",
	}

	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Generate", ctx, input, mock.Anything).Return(expectedResponse, nil)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.Generate(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, expectedResponse, result)
	mockQuota.AssertExpectations(t)
	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_WithTools(t *testing.T) {
	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	tools := []*schema.ToolInfo{{Name: "test_tool"}}
	wrappedModel := new(MockToolCallingChatModel)

	mockModel.On("WithTools", tools).Return(wrappedModel, nil)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.WithTools(tools)

	assert.NoError(t, err)
	assert.NotNil(t, result)

	quotaResult, ok := result.(*QuotaChatModel)
	assert.True(t, ok)
	assert.Equal(t, wrappedModel, quotaResult.inner)

	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_WithTools_Error(t *testing.T) {
	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	tools := []*schema.ToolInfo{{Name: "test_tool"}}
	toolsErr := errors.New("tools error")

	mockModel.On("WithTools", tools).Return(nil, toolsErr)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.WithTools(tools)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, toolsErr)

	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_Stream_CallsQuota(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}

	messages := []*schema.Message{
		{Role: schema.Assistant, Content: "partial"},
		{
			Role:    schema.Assistant,
			Content: "final",
			ResponseMeta: &schema.ResponseMeta{
				Usage: &schema.TokenUsage{
					PromptTokens:     15,
					CompletionTokens: 8,
				},
			},
		},
	}

	sr, sw := schema.Pipe[*schema.Message](10)
	go func() {
		defer sw.Close()
		for _, msg := range messages {
			sw.Send(msg, nil)
		}
	}()

	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Stream", ctx, input, mock.Anything).Return(sr, nil)
	mockQuota.On("AfterLLM", ctx, session, 15, 8)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	stream, err := quotaModel.Stream(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, stream)

	var received []*schema.Message
	for {
		msg, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatalf("unexpected error: %v", recvErr)
		}
		received = append(received, msg)
	}

	assert.Len(t, received, 2)
	mockQuota.AssertExpectations(t)
	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_Stream_QuotaExhausted(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	quotaErr := errors.New("quota exhausted")

	mockQuota.On("BeforeLLM", ctx, session).Return(quotaErr)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.Stream(ctx, input)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, quotaErr)
	mockModel.AssertNotCalled(t, "Stream")
	mockQuota.AssertExpectations(t)
}

func TestQuotaChatModel_Stream_InnerError(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	innerErr := errors.New("stream error")

	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Stream", ctx, input, mock.Anything).Return(nil, innerErr)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.Stream(ctx, input)

	assert.Nil(t, result)
	assert.ErrorIs(t, err, innerErr)
	mockQuota.AssertExpectations(t)
	mockModel.AssertExpectations(t)
}
