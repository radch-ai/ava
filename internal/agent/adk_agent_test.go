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
	task := Task{Message: "hello", MsgID: "msg1"}
	opts := ADKAgentOptions{
		AgentID: "agent1",
		UserID:  "user1",
	}

	agent := NewADKAgent(ctx, task, opts)

	assert.NotNil(t, agent)
	assert.NotNil(t, agent.laneCh)
	assert.Equal(t, "agent1", agent.agentID)
	assert.Equal(t, "user1", agent.userID)
	assert.NotNil(t, agent.connRef)
}

func TestNewADKAgent_QueuesFirstMessage(t *testing.T) {
	ctx := context.Background()
	task := Task{Message: "hello", MsgID: "msg1"}

	agent := NewADKAgent(ctx, task, ADKAgentOptions{})

	assert.Equal(t, 1, len(agent.laneCh))
}

func TestADKAgent_Stop(t *testing.T) {
	ctx := context.Background()
	task := Task{Message: "hello"}

	agent := NewADKAgent(ctx, task, ADKAgentOptions{})

	require.NotNil(t, agent.ctx)
	agent.Stop()

	assert.Error(t, agent.ctx.Err())
}

func TestADKAgent_LaneCh_AcceptsMessages(t *testing.T) {
	ctx := context.Background()
	task := Task{Message: "first"}

	agent := NewADKAgent(ctx, task, ADKAgentOptions{})

	agent.laneCh <- Task{Message: "second"}
	agent.laneCh <- Task{Message: "third"}

	assert.Equal(t, 3, len(agent.laneCh))
}

func TestADKAgent_Loop_DrainsMessages(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Task{Message: "first"}, ADKAgentOptions{})

	agent.laneCh <- Task{Message: "second"}
	agent.laneCh <- Task{Message: "third"}

	var receivedMessages []string
	agent.processTasksFn = func(tasks []Task) {
		for _, t := range tasks {
			receivedMessages = append(receivedMessages, t.Message)
		}
	}

	go agent.loop()

	time.Sleep(100 * time.Millisecond)

	assert.GreaterOrEqual(t, len(receivedMessages), 3)
	agent.Stop()
}

func TestADKAgent_Loop_IdleExit(t *testing.T) {
	ctx := context.Background()
	agent := NewADKAgent(ctx, Task{Message: "first"}, ADKAgentOptions{
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
	agent := NewADKAgent(ctx, Task{Message: "first"}, ADKAgentOptions{
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
	agent := NewADKAgent(ctx, Task{Message: "first"}, ADKAgentOptions{})

	agent.laneCh <- Task{Message: "second"}
	agent.laneCh <- Task{Message: "third"}

	tasks := agent.drainMessages(Task{Message: "new"})

	assert.GreaterOrEqual(t, len(tasks), 1)
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
