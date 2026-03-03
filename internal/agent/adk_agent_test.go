package agent

import (
	"context"
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

func TestNewADKAgent_CreatesLaneCh(t *testing.T) {
	ctx := context.Background()
	msg := Message{Content: "hello", ID: "msg1"}
	opts := ADKAgentOptions{
		AgentID: "agent1",
		UserID:  "user1",
	}

	agent := NewADKAgent(ctx, msg, opts)

	assert.NotNil(t, agent)
	assert.NotNil(t, agent.laneCh)
	assert.Equal(t, "agent1", agent.agentID)
	assert.Equal(t, "user1", agent.userID)
	// conn is nil when not provided
	assert.Nil(t, agent.conn)
}

func TestNewADKAgent_QueuesFirstMessage(t *testing.T) {
	ctx := context.Background()
	msg := Message{Content: "hello", ID: "msg1"}

	agent := NewADKAgent(ctx, msg, ADKAgentOptions{})

	assert.Equal(t, 1, len(agent.laneCh))
}

func TestADKAgent_Stop(t *testing.T) {
	ctx := context.Background()
	msg := Message{Content: "hello"}

	agent := NewADKAgent(ctx, msg, ADKAgentOptions{})

	require.NotNil(t, agent.ctx)
	agent.Stop()

	assert.Error(t, agent.ctx.Err())
}

func TestADKAgent_LaneCh_AcceptsMessages(t *testing.T) {
	ctx := context.Background()
	msg := Message{Content: "first"}

	agent := NewADKAgent(ctx, msg, ADKAgentOptions{})

	agent.laneCh <- Message{Content: "second"}
	agent.laneCh <- Message{Content: "third"}

	assert.Equal(t, 3, len(agent.laneCh))
}

func TestADKAgent_Loop_DrainsMessages(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Message{Content: "first"}, ADKAgentOptions{})

	agent.laneCh <- Message{Content: "second"}
	agent.laneCh <- Message{Content: "third"}

	var receivedMessages []string
	agent.processMessagesFn = func(messages []Message) {
		for _, m := range messages {
			receivedMessages = append(receivedMessages, m.Content)
		}
	}

	go agent.loop()

	time.Sleep(100 * time.Millisecond)

	assert.GreaterOrEqual(t, len(receivedMessages), 3)
	agent.Stop()
}

func TestADKAgent_Loop_IdleExit(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Message{Content: "first"}, ADKAgentOptions{
		IdleTimeout: 50 * time.Millisecond,
	})

	exited := make(chan bool, 1)
	agent.onExit = func() {
		exited <- true
	}

	go agent.loop()

	select {
	case <-exited:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected agent to exit on idle")
	}
}

func TestADKAgent_Loop_ContextCancel(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Message{Content: "first"}, ADKAgentOptions{
		IdleTimeout: 10 * time.Second,
	})

	exited := make(chan bool, 1)
	agent.onExit = func() {
		exited <- true
	}

	go agent.loop()

	time.Sleep(20 * time.Millisecond)
	agent.Stop()

	select {
	case <-exited:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected agent to exit on context cancel")
	}
}

func TestADKAgent_DrainMessages(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Message{Content: "first"}, ADKAgentOptions{})

	agent.laneCh <- Message{Content: "second"}
	agent.laneCh <- Message{Content: "third"}

	messages := agent.drainMessages(Message{Content: "new"})

	assert.GreaterOrEqual(t, len(messages), 1)
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
	tools := GetGlobalTools()

	assert.NotEmpty(t, tools, "GetGlobalTools should return at least one tool")
}

func TestADKAgent_ProcessMessages_CombinesInput(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Message{Content: "first"}, ADKAgentOptions{})

	agent.processMessagesFn = nil

	messages := []Message{
		{Content: "hello", ID: "msg1"},
		{Content: "world", ID: "msg2"},
		{Content: "test", ID: "msg3"},
	}

	agent.processMessages(messages)
}

func TestADKAgent_ProcessMessages_ExtractsID(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Message{Content: "first"}, ADKAgentOptions{})

	agent.processMessagesFn = nil

	messages := []Message{
		{Content: "hello", ID: "msg-123"},
		{Content: "world"},
	}

	agent.processMessages(messages)
}

func TestADKAgent_ProcessMessages_WithNilConn(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Message{Content: "first"}, ADKAgentOptions{})

	agent.processMessagesFn = nil

	// conn is nil when not provided
	assert.Nil(t, agent.conn)

	messages := []Message{
		{Content: "hello", ID: "msg-123"},
	}

	agent.processMessages(messages)
}

func TestADKAgent_ProcessMessages_EmptyMessages(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Message{Content: "first"}, ADKAgentOptions{})

	agent.processMessagesFn = nil

	agent.processMessages([]Message{})
}
