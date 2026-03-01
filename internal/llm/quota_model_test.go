package llm

import (
	"context"
	"errors"
	"io"
	"sync"
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

func TestQuotaChatModel_Stream_ErrorMidStream(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	streamErr := errors.New("connection lost")

	sr, sw := schema.Pipe[*schema.Message](10)
	go func() {
		defer sw.Close()
		sw.Send(&schema.Message{Role: schema.Assistant, Content: "partial"}, nil)
		sw.Send(nil, streamErr)
	}()

	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Stream", ctx, input, mock.Anything).Return(sr, nil)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	stream, err := quotaModel.Stream(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, stream)

	msg1, err1 := stream.Recv()
	assert.NoError(t, err1)
	assert.NotNil(t, msg1)

	msg2, err2 := stream.Recv()
	assert.Nil(t, msg2)
	assert.ErrorIs(t, err2, streamErr)

	mockQuota.AssertExpectations(t)
	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_Stream_UsageInFinalMessage(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1", AgentID: "agent1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}

	sr, sw := schema.Pipe[*schema.Message](10)
	go func() {
		defer sw.Close()
		sw.Send(&schema.Message{Role: schema.Assistant, Content: "thinking..."}, nil)
		sw.Send(&schema.Message{
			Role:    schema.Assistant,
			Content: "final response",
			ResponseMeta: &schema.ResponseMeta{
				Usage: &schema.TokenUsage{
					PromptTokens:     100,
					CompletionTokens: 50,
				},
			},
		}, nil)
	}()

	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Stream", ctx, input, mock.Anything).Return(sr, nil)
	mockQuota.On("AfterLLM", ctx, session, 100, 50)

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

func TestQuotaChatModel_Stream_NoUsage_NoAfterCallback(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}

	sr, sw := schema.Pipe[*schema.Message](10)
	go func() {
		defer sw.Close()
		sw.Send(&schema.Message{Role: schema.Assistant, Content: "response without usage"}, nil)
	}()

	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Stream", ctx, input, mock.Anything).Return(sr, nil)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	stream, err := quotaModel.Stream(ctx, input)

	assert.NoError(t, err)
	assert.NotNil(t, stream)

	for {
		_, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatalf("unexpected error: %v", recvErr)
		}
	}

	mockQuota.AssertExpectations(t)
	mockQuota.AssertNotCalled(t, "AfterLLM", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_Generate_ConcurrentCalls(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1", AgentID: "agent1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	expectedResponse := &schema.Message{
		Role:    schema.Assistant,
		Content: "response",
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

	numGoroutines := 10
	done := make(chan bool, numGoroutines)
	var errors []error
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		go func() {
			result, err := quotaModel.Generate(ctx, input)
			if err != nil {
				mu.Lock()
				errors = append(errors, err)
				mu.Unlock()
			} else {
				assert.NotNil(t, result)
			}
			done <- true
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	assert.Empty(t, errors)
	mockQuota.AssertExpectations(t)
	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_WithTools_PreservesQuotaAndSession(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1", AgentID: "agent1"}

	tools := []*schema.ToolInfo{{Name: "test_tool"}}
	wrappedModel := new(MockToolCallingChatModel)

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	expectedResponse := &schema.Message{
		Role:    schema.Assistant,
		Content: "response",
		ResponseMeta: &schema.ResponseMeta{
			Usage: &schema.TokenUsage{
				PromptTokens:     20,
				CompletionTokens: 10,
			},
		},
	}

	mockModel.On("WithTools", tools).Return(wrappedModel, nil)
	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	wrappedModel.On("Generate", ctx, input, mock.Anything).Return(expectedResponse, nil)
	mockQuota.On("AfterLLM", ctx, session, 20, 10)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.WithTools(tools)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	quotaResult, ok := result.(*QuotaChatModel)
	assert.True(t, ok)
	assert.Equal(t, mockQuota, quotaResult.quota)
	assert.Equal(t, session, quotaResult.session)

	genResult, genErr := quotaResult.Generate(ctx, input)
	assert.NoError(t, genErr)
	assert.Equal(t, expectedResponse, genResult)

	mockModel.AssertExpectations(t)
	wrappedModel.AssertExpectations(t)
	mockQuota.AssertExpectations(t)
}

func TestQuotaChatModel_Generate_WithNilResponseMeta(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	expectedResponse := &schema.Message{
		Role:         schema.Assistant,
		Content:      "response",
		ResponseMeta: nil,
	}

	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Generate", ctx, input, mock.Anything).Return(expectedResponse, nil)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.Generate(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, expectedResponse, result)
	mockQuota.AssertNotCalled(t, "AfterLLM", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockModel.AssertExpectations(t)
}

func TestQuotaChatModel_Generate_WithNilUsage(t *testing.T) {
	ctx := context.Background()

	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}

	input := []*schema.Message{{Role: schema.User, Content: "hello"}}
	expectedResponse := &schema.Message{
		Role:    schema.Assistant,
		Content: "response",
		ResponseMeta: &schema.ResponseMeta{
			Usage: nil,
		},
	}

	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Generate", ctx, input, mock.Anything).Return(expectedResponse, nil)

	quotaModel := NewQuotaChatModel(mockModel, mockQuota, session)

	result, err := quotaModel.Generate(ctx, input)

	assert.NoError(t, err)
	assert.Equal(t, expectedResponse, result)
	mockQuota.AssertNotCalled(t, "AfterLLM", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockModel.AssertExpectations(t)
}
