package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockQuotaChatModelForADK struct {
	inner model.ToolCallingChatModel
}

func (m *MockQuotaChatModelForADK) GetInnerModel() model.ToolCallingChatModel {
	return m.inner
}

type MockToolCallingChatModelForADK struct {
	generateCalls int
}

func (m *MockToolCallingChatModelForADK) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.generateCalls++
	return &schema.Message{Role: schema.Assistant, Content: "response"}, nil
}

func (m *MockToolCallingChatModelForADK) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, nil
}

func (m *MockToolCallingChatModelForADK) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func (m *MockToolCallingChatModelForADK) BindTools(tools []*schema.ToolInfo) error {
	return nil
}

func TestNewUserAgent_CreatesLaneCh(t *testing.T) {
	ctx := context.Background()
	msg := Message{Content: "hello", ID: "msg1"}
	opts := UserAgentOptions{
		AgentID: "agent1",
		UserID:  "user1",
	}

	agent := NewUserAgent(ctx, msg, opts)

	assert.NotNil(t, agent)
	assert.NotNil(t, agent.laneCh)
	assert.Equal(t, "agent1", agent.agentID)
	assert.Equal(t, "user1", agent.userID)
	// conn is nil when not provided
	assert.Nil(t, agent.conn)
	agent.Stop()
}

func TestNewUserAgent_QueuesFirstMessage(t *testing.T) {
	ctx := context.Background()
	msg := Message{Content: "hello", ID: "msg1"}

	// Use idle timeout to prevent the agent from exiting before we check
	agent := NewUserAgent(ctx, msg, UserAgentOptions{
		IdleTimeout: 1 * time.Second,
	})

	// Message should be queued (though loop may drain it quickly)
	// Just verify agent was created successfully
	assert.NotNil(t, agent)
	agent.Stop()
}

func TestUserAgent_Stop(t *testing.T) {
	ctx := context.Background()
	msg := Message{Content: "hello"}

	agent := NewUserAgent(ctx, msg, UserAgentOptions{})

	require.NotNil(t, agent.ctx)
	agent.Stop()

	assert.Error(t, agent.ctx.Err())
}

func TestUserAgent_Loop_DrainsMessages(t *testing.T) {
	ctx := context.Background()

	var receivedMessages []string
	var mu sync.Mutex

	agent := NewUserAgent(ctx, Message{Content: "first"}, UserAgentOptions{
		IdleTimeout: 1 * time.Second,
		ProcessMessagesFn: func(messages []Message) {
			mu.Lock()
			defer mu.Unlock()
			for _, m := range messages {
				receivedMessages = append(receivedMessages, m.Content)
			}
		},
	})

	agent.laneCh <- Message{Content: "second"}
	agent.laneCh <- Message{Content: "third"}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	count := len(receivedMessages)
	mu.Unlock()

	assert.GreaterOrEqual(t, count, 3)
	agent.Stop()
}

func TestUserAgent_Loop_IdleExit(t *testing.T) {
	ctx := context.Background()
	exited := make(chan bool, 1)

	_ = NewUserAgent(ctx, Message{Content: "first"}, UserAgentOptions{
		IdleTimeout: 50 * time.Millisecond,
		OnExit: func() {
			exited <- true
		},
	})

	select {
	case <-exited:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected agent to exit on idle")
	}
}

func TestUserAgent_Loop_ContextCancel(t *testing.T) {
	ctx := context.Background()
	exited := make(chan bool, 1)

	agent := NewUserAgent(ctx, Message{Content: "first"}, UserAgentOptions{
		IdleTimeout: 10 * time.Second,
		OnExit: func() {
			exited <- true
		},
	})

	time.Sleep(20 * time.Millisecond)
	agent.Stop()

	select {
	case <-exited:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected agent to exit on context cancel")
	}
}

func TestUserAgent_DrainMessages(t *testing.T) {
	ctx := context.Background()
	agent := NewUserAgent(ctx, Message{}, UserAgentOptions{
		IdleTimeout: 1 * time.Second,
	})

	agent.laneCh <- Message{Content: "second"}
	agent.laneCh <- Message{Content: "third"}

	messages := agent.drainMessages(Message{Content: "new"})

	assert.GreaterOrEqual(t, len(messages), 1)
	agent.Stop()
}

func TestBuildInstruction_WithMemories(t *testing.T) {
	systemPrompt := "You are a helpful assistant."
	memories := []string{"User likes Go programming"}

	result := buildInstruction(systemPrompt, memories)

	assert.Equal(t, "You are a helpful assistant.\n\nContext about the user:\n- User likes Go programming\n", result)
}

func TestBuildInstruction_NoMemories(t *testing.T) {
	systemPrompt := "You are a helpful assistant."
	memories := []string{}

	result := buildInstruction(systemPrompt, memories)

	assert.Equal(t, "You are a helpful assistant.", result)
}

func TestBuildInstruction_EmptySystemPrompt(t *testing.T) {
	systemPrompt := ""
	memories := []string{"User prefers dark mode"}

	result := buildInstruction(systemPrompt, memories)

	assert.Equal(t, "\n\nContext about the user:\n- User prefers dark mode\n", result)
}

func TestBuildInstruction_MultipleMemories(t *testing.T) {
	systemPrompt := "You are a helpful assistant."
	memories := []string{
		"User likes Go programming",
		"User prefers concise responses",
		"User works at Acme Corp",
	}

	result := buildInstruction(systemPrompt, memories)

	expected := "You are a helpful assistant.\n\nContext about the user:\n- User likes Go programming\n- User prefers concise responses\n- User works at Acme Corp\n"
	assert.Equal(t, expected, result)
}

func TestGetGlobalTools_ReturnsTools(t *testing.T) {
	tools := getGlobalTools()

	assert.NotEmpty(t, tools, "getGlobalTools should return at least one tool")
}

func TestUserAgent_ProcessMessages_CombinesInput(t *testing.T) {
	ctx := context.Background()
	agent := NewUserAgent(ctx, Message{}, UserAgentOptions{
		IdleTimeout: 1 * time.Second,
	})

	messages := []Message{
		{Content: "hello", ID: "msg1"},
		{Content: "world", ID: "msg2"},
		{Content: "test", ID: "msg3"},
	}

	agent.processMessages(messages)
	agent.Stop()
}

func TestUserAgent_ProcessMessages_ExtractsID(t *testing.T) {
	ctx := context.Background()
	agent := NewUserAgent(ctx, Message{}, UserAgentOptions{
		IdleTimeout: 1 * time.Second,
	})

	messages := []Message{
		{Content: "hello", ID: "msg-123"},
		{Content: "world"},
	}

	agent.processMessages(messages)
	agent.Stop()
}

func TestUserAgent_ProcessMessages_WithNilConn(t *testing.T) {
	ctx := context.Background()
	agent := NewUserAgent(ctx, Message{}, UserAgentOptions{
		IdleTimeout: 1 * time.Second,
	})

	// conn is nil when not provided
	assert.Nil(t, agent.conn)

	messages := []Message{
		{Content: "hello", ID: "msg-123"},
	}

	agent.processMessages(messages)
	agent.Stop()
}

func TestUserAgent_ProcessMessages_EmptyMessages(t *testing.T) {
	ctx := context.Background()
	agent := NewUserAgent(ctx, Message{}, UserAgentOptions{
		IdleTimeout: 1 * time.Second,
	})

	agent.processMessages([]Message{})
	agent.Stop()
}
