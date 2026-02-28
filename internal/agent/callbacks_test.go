package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockWSConn struct {
	mock.Mock
}

func (m *MockWSConn) WriteJSON(v interface{}) error {
	args := m.Called(v)
	return args.Error(0)
}

func TestBuildProgressHandler_ModelOnStart_EmitsThinking(t *testing.T) {
	mockConn := new(MockWSConn)
	expectedEvent := ProgressEvent{Type: "thinking"}
	mockConn.On("WriteJSON", expectedEvent).Return(nil)

	pc := BuildProgressHandler(mockConn)
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{Name: "test", Component: components.ComponentOfChatModel}
	input := &model.CallbackInput{Messages: nil}

	resultCtx := pc.ChatModelHandler.OnStart(ctx, runInfo, input)

	assert.Equal(t, ctx, resultCtx)
	mockConn.AssertExpectations(t)
}

func TestBuildProgressHandler_ToolOnStart_EmitsToolStart(t *testing.T) {
	mockConn := new(MockWSConn)
	expectedEvent := ProgressEvent{
		Type: "tool_start",
		Payload: map[string]interface{}{
			"tool_name": "test_tool",
			"args":      `{"arg1":"value1"}`,
		},
	}
	mockConn.On("WriteJSON", expectedEvent).Return(nil)

	pc := BuildProgressHandler(mockConn)
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{Name: "test_tool", Component: components.ComponentOfTool}
	input := &tool.CallbackInput{ArgumentsInJSON: `{"arg1":"value1"}`}

	resultCtx := pc.ToolHandler.OnStart(ctx, runInfo, input)

	assert.Equal(t, ctx, resultCtx)
	mockConn.AssertExpectations(t)
}

func TestBuildProgressHandler_ToolOnEnd_EmitsToolEnd(t *testing.T) {
	mockConn := new(MockWSConn)
	expectedEvent := ProgressEvent{
		Type: "tool_end",
		Payload: map[string]interface{}{
			"tool_name": "test_tool",
			"result":    "tool result",
		},
	}
	mockConn.On("WriteJSON", expectedEvent).Return(nil)

	pc := BuildProgressHandler(mockConn)
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{Name: "test_tool", Component: components.ComponentOfTool}
	output := &tool.CallbackOutput{Response: "tool result"}

	resultCtx := pc.ToolHandler.OnEnd(ctx, runInfo, output)

	assert.Equal(t, ctx, resultCtx)
	mockConn.AssertExpectations(t)
}

func TestBuildProgressHandler_NilConn_SkipsWrites(t *testing.T) {
	pc := BuildProgressHandler(nil)
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{Name: "test", Component: components.ComponentOfChatModel}
	input := &model.CallbackInput{Messages: nil}

	resultCtx := pc.ChatModelHandler.OnStart(ctx, runInfo, input)

	assert.Equal(t, ctx, resultCtx)
}

func TestBuildProgressHandler_NilConn_ToolOnStart_SkipsWrites(t *testing.T) {
	pc := BuildProgressHandler(nil)
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{Name: "test_tool", Component: components.ComponentOfTool}
	input := &tool.CallbackInput{ArgumentsInJSON: `{}`}

	resultCtx := pc.ToolHandler.OnStart(ctx, runInfo, input)

	assert.Equal(t, ctx, resultCtx)
}

func TestBuildProgressHandler_NilConn_ToolOnEnd_SkipsWrites(t *testing.T) {
	pc := BuildProgressHandler(nil)
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{Name: "test_tool", Component: components.ComponentOfTool}
	output := &tool.CallbackOutput{Response: "result"}

	resultCtx := pc.ToolHandler.OnEnd(ctx, runInfo, output)

	assert.Equal(t, ctx, resultCtx)
}

func TestProgressCallbacks_ToHandler_ReturnsValidHandler(t *testing.T) {
	mockConn := new(MockWSConn)
	pc := BuildProgressHandler(mockConn)

	handler := pc.ToHandler()

	assert.NotNil(t, handler)
}

func TestProgressEvent_JSONMarshal(t *testing.T) {
	event := ProgressEvent{
		Type: "tool_start",
		Payload: map[string]interface{}{
			"tool_name": "test",
			"args":      `{"key":"value"}`,
		},
	}

	data, err := json.Marshal(event)
	assert.NoError(t, err)

	var unmarshaled ProgressEvent
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, event.Type, unmarshaled.Type)
}

func TestBuildProgressHandler_ToolOnEnd_WithToolOutput(t *testing.T) {
	mockConn := new(MockWSConn)
	mockConn.On("WriteJSON", mock.AnythingOfType("agent.ProgressEvent")).Return(nil)

	pc := BuildProgressHandler(mockConn)
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{Name: "test_tool", Component: components.ComponentOfTool}
	output := &tool.CallbackOutput{
		Response:   "",
		ToolOutput: nil,
		Extra:      map[string]any{"custom": "data"},
	}

	resultCtx := pc.ToolHandler.OnEnd(ctx, runInfo, output)

	assert.Equal(t, ctx, resultCtx)
	mockConn.AssertExpectations(t)
}

func TestProgressCallbacks_ImplementsWSConn(t *testing.T) {
	var _ WSConn = (*MockWSConn)(nil)
}
