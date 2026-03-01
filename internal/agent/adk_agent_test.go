package agent

import (
	"context"
	"testing"

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
