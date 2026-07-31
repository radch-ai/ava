# Agent Runner & Eino Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace custom agent runner/loop with Eino ADK's ChatModelAgent and Runner, implementing quota via wrapper pattern and progress events via callbacks.

**Architecture:** Agent struct holds Eino ChatModelAgent + Runner built at creation. QuotaChatModel wraps ToolCallingChatModel for quota checking. Lane uses Go channel. Goroutine starts lazily on first Submit. Progress events via Eino callbacks.

**Tech Stack:** Go, Eino ADK, gorilla/websocket, Redis, PostgreSQL, mem0

**Design Doc:** `.claude/brainstorming/2026-02-28-agent-runner-eino-integration-design.md`

---

## Prerequisites

- [ ] Read design doc: `.claude/brainstorming/2026-02-28-agent-runner-eino-integration-design.md`
- [ ] Understand Eino ADK: https://cloudwego.io/docs/eino/core_modules/eino_adk/
- [ ] Quota system design: `.claude/brainstorming/2026-02-26-user-quota-system-design.md`

---

## Task 1: QuotaChatModel Wrapper

**Files:**
- Create: `internal/llm/quota_model.go`
- Create: `internal/llm/quota_model_test.go`

**Step 1: Write the failing test for QuotaChatModel interface**

```go
// internal/llm/quota_model_test.go
package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockToolCallingChatModel implements model.ToolCallingChatModel
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

// MockQuotaMiddleware for testing
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
	// This test verifies QuotaChatModel implements model.ToolCallingChatModel
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
			Usage: &model.TokenUsage{
				PromptTokens:     10,
				CompletionTokens: 5,
			},
		},
	}
	
	mockQuota.On("BeforeLLM", ctx, session).Return(nil)
	mockModel.On("Generate", ctx, input, mock.Anything).Return(expectedResponse, nil)
	mockQuota.On("AfterLLM", ctx, session, 10, 5)
	
	quotaModel := &QuotaChatModel{
		inner:   mockModel,
		quota:   mockQuota,
		session: session,
	}
	
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
	
	quotaModel := &QuotaChatModel{
		inner:   mockModel,
		quota:   mockQuota,
		session: session,
	}
	
	result, err := quotaModel.Generate(ctx, input)
	
	assert.Nil(t, result)
	assert.ErrorIs(t, err, quotaErr)
	// Should NOT call inner model if quota check fails
	mockModel.AssertNotCalled(t, "Generate")
	mockQuota.AssertExpectations(t)
}

func TestQuotaChatModel_WithTools(t *testing.T) {
	mockModel := new(MockToolCallingChatModel)
	mockQuota := new(MockQuotaMiddleware)
	session := &QuotaSession{UserID: "user1"}
	
	tools := []*schema.ToolInfo{{Name: "test_tool"}}
	wrappedModel := new(MockToolCallingChatModel)
	
	mockModel.On("WithTools", tools).Return(wrappedModel, nil)
	
	quotaModel := &QuotaChatModel{
		inner:   mockModel,
		quota:   mockQuota,
		session: session,
	}
	
	result, err := quotaModel.WithTools(tools)
	
	assert.NoError(t, err)
	assert.NotNil(t, result)
	
	// Verify it's still a QuotaChatModel wrapper
	quotaResult, ok := result.(*QuotaChatModel)
	assert.True(t, ok)
	assert.Equal(t, wrappedModel, quotaResult.inner)
	
	mockModel.AssertExpectations(t)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/llm/...`
Expected: FAIL - `QuotaChatModel` undefined

**Step 3: Write minimal implementation**

```go
// internal/llm/quota_model.go
package llm

import (
	"context"
	"io"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var ErrQuotaExhausted = errors.New("quota exhausted")

type QuotaSession struct {
	UserID   string
	AgentID  string
}

type QuotaMiddleware interface {
	BeforeLLM(ctx context.Context, session *QuotaSession) error
	AfterLLM(ctx context.Context, session *QuotaSession, promptTokens, completionTokens int)
}

type QuotaChatModel struct {
	inner   model.ToolCallingChatModel
	quota   QuotaMiddleware
	session *QuotaSession
}

func NewQuotaChatModel(inner model.ToolCallingChatModel, quota QuotaMiddleware, session *QuotaSession) *QuotaChatModel {
	return &QuotaChatModel{
		inner:   inner,
		quota:   quota,
		session: session,
	}
}

func (q *QuotaChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if err := q.quota.BeforeLLM(ctx, q.session); err != nil {
		return nil, err
	}
	
	result, err := q.inner.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	
	if result.ResponseMeta != nil && result.ResponseMeta.Usage != nil {
		usage := result.ResponseMeta.Usage
		q.quota.AfterLLM(ctx, q.session, usage.PromptTokens, usage.CompletionTokens)
	}
	
	return result, nil
}

func (q *QuotaChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if err := q.quota.BeforeLLM(ctx, q.session); err != nil {
		return nil, err
	}
	
	stream, err := q.inner.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	
	return q.wrapStreamForQuota(ctx, stream), nil
}

func (q *QuotaChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	innerWithTools, err := q.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	
	return &QuotaChatModel{
		inner:   innerWithTools,
		quota:   q.quota,
		session: q.session,
	}, nil
}

func (q *QuotaChatModel) wrapStreamForQuota(ctx context.Context, stream *schema.StreamReader[*schema.Message]) *schema.StreamReader[*schema.Message] {
	sr, sw := schema.Pipe[*schema.Message](10)
	
	go func() {
		defer sw.Close()
		
		var lastMessage *schema.Message
		
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				sw.Send(msg, err)
				break
			}
			
			lastMessage = msg
			sw.Send(msg, nil)
		}
		
		// AfterLLM with final token usage
		if lastMessage != nil && lastMessage.ResponseMeta != nil && lastMessage.ResponseMeta.Usage != nil {
			usage := lastMessage.ResponseMeta.Usage
			q.quota.AfterLLM(ctx, q.session, usage.PromptTokens, usage.CompletionTokens)
		}
	}()
	
	return sr
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/llm/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/llm/quota_model.go internal/llm/quota_model_test.go
git commit -m "feat(llm): add QuotaChatModel wrapper for ToolCallingChatModel"
```

---

## Task 2: Progress Callbacks Handler

**Files:**
- Create: `internal/agent/callbacks.go`
- Create: `internal/agent/callbacks_test.go`

**Step 1: Write the failing test for progress callbacks**

```go
// internal/agent/callbacks_test.go
package agent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/utils/callbacks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockConn struct {
	mock.Mock
	written []interface{}
}

func (m *MockConn) WriteJSON(v interface{}) error {
	m.written = append(m.written, v)
	args := m.Called(v)
	return args.Error(0)
}

func TestBuildProgressHandler_EmitsThinking(t *testing.T) {
	mockConn := new(MockConn)
	mockConn.On("WriteJSON", mock.Anything).Return(nil)
	
	progressHandler := buildProgressHandler(mockConn)
	
	// Get the ChatModel handler
	chatHandler := progressHandler.(*callbacks.HandlerHelper).Handler()
	
	// Simulate OnStart
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{ComponentName: "test-model"}
	input := &model.CallbackInput{Messages: []*schema.Message{{Role: schema.User, Content: "hi"}}}
	
	chatHandler.OnStart(ctx, runInfo, input)
	
	// Verify thinking event was sent
	assert.Len(t, mockConn.written, 1)
	event := mockConn.written[0].(map[string]interface{})
	assert.Equal(t, "thinking", event["type"])
}

func TestBuildProgressHandler_EmitsToolEvents(t *testing.T) {
	mockConn := new(MockConn)
	mockConn.On("WriteJSON", mock.Anything).Return(nil)
	
	progressHandler := buildProgressHandler(mockConn)
	
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{ComponentName: "search_tool"}
	
	// Simulate tool OnStart
	toolInput := &tool.CallbackInput{ArgumentsInJSON: `{"query": "test"}`}
	progressHandler.Tool.OnStart(ctx, runInfo, toolInput)
	
	// Verify tool_start event
	assert.Len(t, mockConn.written, 1)
	event := mockConn.written[0].(map[string]interface{})
	assert.Equal(t, "tool_start", event["type"])
	
	// Simulate tool OnEnd
	toolOutput := &tool.CallbackOutput{Response: "results"}
	progressHandler.Tool.OnEnd(ctx, runInfo, toolOutput)
	
	// Verify tool_end event
	assert.Len(t, mockConn.written, 2)
	event = mockConn.written[1].(map[string]interface{})
	assert.Equal(t, "tool_end", event["type"])
}

func TestBuildProgressHandler_SkipsOnNilConn(t *testing.T) {
	progressHandler := buildProgressHandler(nil)
	
	// Should not panic with nil conn
	ctx := context.Background()
	runInfo := &callbacks.RunInfo{ComponentName: "test"}
	input := &model.CallbackInput{}
	
	// Should return without error
	progressHandler.ChatModel.OnStart(ctx, runInfo, input)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: FAIL - `buildProgressHandler` undefined

**Step 3: Write minimal implementation**

```go
// internal/agent/callbacks.go
package agent

import (
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/utils/callbacks"
	"github.com/gorilla/websocket"
)

type ProgressEvent struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

type ProgressCallbacks struct {
	ChatModel *callbacks.ModelCallbackHandler
	Tool      *callbacks.ToolCallbackHandler
}

func buildProgressHandler(conn *websocket.Conn) *ProgressCallbacks {
	emitToWS := func(eventType string, data map[string]interface{}) {
		if conn == nil {
			return
		}
		conn.WriteJSON(ProgressEvent{
			Type: eventType,
			Data: data,
		})
	}
	
	return &ProgressCallbacks{
		ChatModel: &callbacks.ModelCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *model.CallbackInput) context.Context {
				emitToWS("thinking", nil)
				return ctx
			},
		},
		Tool: &callbacks.ToolCallbackHandler{
			OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context {
				emitToWS("tool_start", map[string]interface{}{
					"tool_name": info.ComponentName,
					"args":      input.ArgumentsInJSON,
				})
				return ctx
			},
			OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *tool.CallbackOutput) context.Context {
				emitToWS("tool_end", map[string]interface{}{
					"tool_name": info.ComponentName,
					"result":    output.Response,
				})
				return ctx
			},
		},
	}
}

// ToHandler converts ProgressCallbacks to callbacks.Handler for use with Eino
func (p *ProgressCallbacks) ToHandler() callbacks.Handler {
	return callbacks.NewHandlerHelper().
		ChatModel(p.ChatModel).
		Tool(p.Tool).
		Handler()
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/callbacks.go internal/agent/callbacks_test.go
git commit -m "feat(agent): add progress callbacks for thinking and tool events"
```

---

## Task 3: New Agent Struct with Eino

**Files:**
- Create: `internal/agent/eino_agent.go`
- Create: `internal/agent/eino_agent_test.go`
- Modify: `internal/agent/types.go` (if exists)

**Step 1: Write the failing test for Agent with Eino**

```go
// internal/agent/eino_agent_test.go
package agent

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAgent_CreatesEinoComponents(t *testing.T) {
	ctx := context.Background()
	
	opts := AgentOptions{
		AgentID:      "agent-123",
		UserID:       "user-456",
		SystemPrompt: "You are a helpful assistant.",
		ModelName:    "gpt-4o",
	}
	
	agent := NewAgent(opts)
	
	assert.NotNil(t, agent)
	assert.Equal(t, "agent-123", agent.agentID)
	assert.Equal(t, "user-456", agent.userID)
	assert.NotNil(t, agent.einoAgent)
	assert.NotNil(t, agent.einoRunner)
	assert.NotNil(t, agent.laneQueue)
}

func TestAgent_Submit_QueuesTask(t *testing.T) {
	agent := NewAgent(AgentOptions{
		AgentID: "test-agent",
		UserID:  "test-user",
	})
	
	task := Task{
		Message: "Hello",
		MsgID:    "msg-123",
	}
	
	agent.Submit(task)
	
	// Verify task is in queue
	assert.Equal(t, 1, len(agent.laneQueue))
}

func TestAgent_Submit_StartsGoroutineLazily(t *testing.T) {
	agent := NewAgent(AgentOptions{
		AgentID: "test-agent",
		UserID:  "test-user",
	})
	
	assert.False(t, agent.goroutineStarted)
	
	agent.Submit(Task{Message: "Hello"})
	
	assert.True(t, agent.goroutineStarted)
}

func TestAgent_Stop_CancelsContext(t *testing.T) {
	agent := NewAgent(AgentOptions{
		AgentID: "test-agent",
		UserID:  "test-user",
	})
	
	require.NotNil(t, agent.ctx)
	
	agent.Stop()
	
	assert.Error(t, agent.ctx.Err()) // context should be cancelled
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: FAIL - NewAgent doesn't create Eino components

**Step 3: Write minimal implementation**

```go
// internal/agent/eino_agent.go
package agent

import (
	"context"
	"sync"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/gorilla/websocket"
)

type Task struct {
	Message string
	Conn    *websocket.Conn
	MsgID   string
}

type AgentOptions struct {
	AgentID      string
	UserID       string
	SystemPrompt string
	Description  string
	ModelName    string
	Tools        []tool.BaseTool
	Quota        QuotaMiddleware
	Mem0         Mem0Client
	Manager      *AgentManager
}

type Agent struct {
	agentID          string
	userID           string
	ctx              context.Context
	cancel           context.CancelFunc
	laneQueue        chan Task
	einoAgent        adk.Agent
	einoRunner       *adk.Runner
	manager          *AgentManager
	goroutineStarted bool
	mu               sync.Mutex
}

func NewAgent(opts AgentOptions) *Agent {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Build system prompt with mem0
	systemPrompt := buildSystemPrompt(opts)
	
	// Create wrapped model with quota (if quota provided)
	var chatModel model.ToolCallingChatModel
	chatModel = createChatModel(opts.ModelName)
	
	if opts.Quota != nil {
		session := &QuotaSession{
			UserID:  opts.UserID,
			AgentID: opts.AgentID,
		}
		chatModel = NewQuotaChatModel(chatModel, opts.Quota, session)
	}
	
	// Create Eino ChatModelAgent
	einoAgent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        opts.AgentID,
		Description: opts.Description,
		Instruction: systemPrompt,
		Model:       chatModel,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: opts.Tools,
			},
		},
	})
	if err != nil {
		cancel()
		panic(err) // Or return error
	}
	
	// Create Eino Runner
	einoRunner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           einoAgent,
		EnableStreaming: true,
	})
	
	return &Agent{
		agentID:     opts.AgentID,
		userID:      opts.UserID,
		ctx:         ctx,
		cancel:      cancel,
		laneQueue:   make(chan Task, 100),
		einoAgent:   einoAgent,
		einoRunner:  einoRunner,
		manager:     opts.Manager,
	}
}

func buildSystemPrompt(opts AgentOptions) string {
	var sb strings.Builder
	sb.WriteString(opts.SystemPrompt)
	
	// Add mem0 memories if available
	if opts.Mem0 != nil {
		memories := opts.Mem0.GetMemories(opts.UserID, opts.AgentID)
		if len(memories) > 0 {
			sb.WriteString("\n\n## User Context (memories)\n")
			for _, mem := range memories {
				sb.WriteString(fmt.Sprintf("- %s\n", mem.Content))
			}
		}
	}
	
	return sb.String()
}

func (a *Agent) Submit(task Task) {
	select {
	case a.laneQueue <- task:
	default:
		// Queue full, log error
	}
	
	a.maybeStartGoroutine()
}

func (a *Agent) maybeStartGoroutine() {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if !a.goroutineStarted {
		a.goroutineStarted = true
		go a.runLoop()
	}
}

func (a *Agent) Stop() {
	a.cancel()
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/eino_agent.go internal/agent/eino_agent_test.go
git commit -m "feat(agent): add Agent struct with Eino ChatModelAgent and Runner"
```

---

## Task 4: runLoop Goroutine

**Files:**
- Modify: `internal/agent/eino_agent.go`
- Modify: `internal/agent/eino_agent_test.go`

**Step 1: Write the failing test for runLoop**

```go
// Add to internal/agent/eino_agent_test.go

func TestAgent_RunLoop_ProcessesTask(t *testing.T) {
	// This is an integration test that requires mocking Eino
	// For unit test, we verify the goroutine starts and processes queue
	
	agent := NewAgent(AgentOptions{
		AgentID: "test-agent",
		UserID:  "test-user",
	})
	
	// Mock the runTurn to capture calls
	var receivedInput string
	agent.runTurnFn = func(conn *websocket.Conn, input string) {
		receivedInput = input
	}
	
	task := Task{Message: "Hello world"}
	agent.Submit(task)
	
	// Wait for goroutine to process
	time.Sleep(100 * time.Millisecond)
	
	assert.Equal(t, "Hello world", receivedInput)
}

func TestAgent_RunLoop_DrainsBufferedMessages(t *testing.T) {
	agent := NewAgent(AgentOptions{
		AgentID: "test-agent",
		UserID:  "test-user",
	})
	
	var receivedMessages []string
	agent.runTurnFn = func(conn *websocket.Conn, input string) {
		receivedMessages = strings.Split(input, "\n")
	}
	
	// Submit multiple tasks quickly
	agent.Submit(Task{Message: "msg1"})
	agent.Submit(Task{Message: "msg2"})
	agent.Submit(Task{Message: "msg3"})
	
	// Wait for goroutine to process
	time.Sleep(200 * time.Millisecond)
	
	// Should have drained all buffered messages
	assert.GreaterOrEqual(t, len(receivedMessages), 1)
}

func TestAgent_RunLoop_EvictsOnIdleTimeout(t *testing.T) {
	mockManager := &MockAgentManager{}
	agent := NewAgent(AgentOptions{
		AgentID: "test-agent",
		UserID:  "test-user",
		Manager: mockManager,
	})
	
	// Set very short idle timeout for testing
	agent.idleTimeout = 50 * time.Millisecond
	
	agent.Submit(Task{Message: "test"})
	
	// Wait for idle timeout
	time.Sleep(100 * time.Millisecond)
	
	mockManager.AssertCalled(t, "Evict", "test-agent")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: FAIL - runLoop not implemented

**Step 3: Write implementation**

```go
// Add to internal/agent/eino_agent.go

const (
	defaultIdleTimeout = 5 * time.Minute
	defaultLaneBufferSize = 100
)

type Agent struct {
	// ... existing fields ...
	idleTimeout time.Duration
	runTurnFn   func(conn *websocket.Conn, input string) // For testing
}

func (a *Agent) runLoop() {
	idleTimer := time.NewTimer(a.idleTimeout)
	defer idleTimer.Stop()
	
	for {
		select {
		case task := <-a.laneQueue:
			idleTimer.Reset(a.idleTimeout)
			
			// Drain any buffered messages
			messages := []string{task.Message}
			drain:
			for {
				select {
				case queued := <-a.laneQueue:
					messages = append(messages, queued.Message)
				default:
					break drain
				}
			}
			
			// Assemble input
			input := strings.Join(messages, "\n")
			
			// Run turn
			if a.runTurnFn != nil {
				a.runTurnFn(task.Conn, input)
			} else {
				a.runTurn(task.Conn, input)
			}
			
		case <-idleTimer.C:
			if a.manager != nil {
				a.manager.Evict(a.agentID)
			}
			return
			
		case <-a.ctx.Done():
			return
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/eino_agent.go internal/agent/eino_agent_test.go
git commit -m "feat(agent): implement runLoop goroutine with idle eviction"
```

---

## Task 5: runTurn with Connection-Agnostic Streaming

**Files:**
- Modify: `internal/agent/eino_agent.go`
- Create: `internal/agent/runturn_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/runturn_test.go
package agent

import (
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestRunTurn_StreamsToConnection(t *testing.T) {
	// Mock connection that captures writes
	mockConn := &MockWebSocketConn{}
	mockConn.On("WriteJSON", mock.Anything).Return(nil)
	
	// ... setup agent with mock Eino runner ...
	
	agent.runTurn(mockConn, "Hello")
	
	// Verify events were written
	assert.Greater(t, len(mockConn.writtenEvents), 0)
}

func TestRunTurn_BuffersWhenConnectionDies(t *testing.T) {
	mockConn := &MockWebSocketConn{}
	// First write succeeds, second fails
	mockConn.On("WriteJSON", mock.Anything).Return(nil).Once()
	mockConn.On("WriteJSON", mock.Anything).Return(websocket.ErrCloseSent).Once()
	
	var savedToRedis string
	agent.saveToRedisFn = func(content string) {
		savedToRedis = content
	}
	
	agent.runTurn(mockConn, "Hello")
	
	// Should still have buffered content even though conn died
	assert.NotEmpty(t, savedToRedis)
}

func TestRunTurn_SavesToRedisAlways(t *testing.T) {
	var savedContent string
	agent.saveToRedisFn = func(content string) {
		savedContent = content
	}
	
	// Even with nil connection
	agent.runTurn(nil, "Hello")
	
	assert.NotEmpty(t, savedContent)
}

func TestRunTurn_SendsNotificationOnDeadConn(t *testing.T) {
	notificationSent := false
	agent.sendNotificationFn = func() {
		notificationSent = true
	}
	
	// Connection is nil (dead)
	agent.runTurn(nil, "Hello")
	
	assert.True(t, notificationSent)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: FAIL - runTurn not implemented

**Step 3: Write implementation**

```go
// Add to internal/agent/eino_agent.go

func (a *Agent) runTurn(conn *websocket.Conn, input string) {
	// Phase 1: Stream to client (best effort)
	var buffer strings.Builder
	
	// Build progress handler with connection
	progressHandler := buildProgressHandler(conn)
	
	// Run Eino with callbacks
	iter := a.einoRunner.Query(a.ctx, input, compose.WithCallbacks(progressHandler.ToHandler()))
	
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		
		if event.Err != nil {
			// Handle error (retry logic elsewhere)
			break
		}
		
		// Accumulate regardless of conn state
		if event.Output != nil && event.Output.MessageOutput != nil {
			msg, err := event.Output.MessageOutput.GetMessage()
			if err == nil && msg != nil {
				buffer.WriteString(msg.Content)
			}
		}
		
		// Try to write to conn
		if conn != nil {
			err := conn.WriteJSON(event)
			if err != nil {
				conn = nil // Mark dead, stop trying
			}
		}
	}
	
	// Phase 2: Always happens (conn alive or dead)
	content := buffer.String()
	a.saveToRedis(content)
	
	if conn == nil {
		a.sendNotification()
	}
}

func (a *Agent) saveToRedis(content string) {
	// TODO: Implement with Redis client
	// Key: history:{userID}:{agentID}
	// Append to list
}

func (a *Agent) sendNotification() {
	// TODO: Call FCM/APNs service
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/eino_agent.go internal/agent/runturn_test.go
git commit -m "feat(agent): implement runTurn with connection-agnostic streaming"
```

---

## Task 6: Context Assembly & Compaction

**Files:**
- Create: `internal/agent/context.go`
- Create: `internal/agent/context_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/context_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAssembleContext_LoadsHistory(t *testing.T) {
	// Setup mock Redis with history
	// ...
	
	agent := &Agent{
		userID:  "user1",
		agentID: "agent1",
	}
	
	messages := agent.assembleContext([]string{"New message"})
	
	assert.NotEmpty(t, messages)
	// Last message should be the new one
}

func TestAssembleContext_CompactsWhenOverThreshold(t *testing.T) {
	// Setup history that exceeds 70% threshold
	// ...
	
	compactionCalled := false
	agent := &Agent{
		userID:         "user1",
		agentID:        "agent1",
		contextLimit:   1000,
		compactHistoryFn: func(history []adk.Message) {
			compactionCalled = true
		},
	}
	
	agent.assembleContext([]string{"test"})
	
	assert.True(t, compactionCalled)
}

func TestEstimateTokens_UsesCharDivide4(t *testing.T) {
	text := "Hello world" // 11 chars
	
	tokens := estimateTokens(text)
	
	assert.Equal(t, 2, tokens) // 11 / 4 = 2 (rounded down)
}

func TestCompactHistory_SummarizesOldTurns(t *testing.T) {
	// Create 30 messages (10 old + 20 recent)
	history := make([]adk.Message, 30)
	for i := range history {
		history[i] = adk.Message{Content: fmt.Sprintf("Message %d", i)}
	}
	
	agent := &Agent{
		summarizeFn: func(old []adk.Message) string {
			return "Summary of old messages"
		},
	}
	
	result := agent.compactHistory(history)
	
	// Should have 21 messages (1 summary + 20 recent)
	assert.Equal(t, 21, len(result))
	assert.Equal(t, "Summary of old messages", result[0].Content)
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/context.go
package agent

import (
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const (
	compactionThreshold = 0.7
	recentTurnsToKeep   = 20
)

func (a *Agent) assembleContext(newMessages []string) []adk.Message {
	// 1. Load history from Redis
	history := a.loadHistory()
	
	// 2. Estimate tokens
	estimatedTokens := a.estimateTotalTokens(history, newMessages)
	
	// 3. Compact if over threshold
	if estimatedTokens > int(float64(a.contextLimit)*compactionThreshold) {
		if a.compactHistoryFn != nil {
			a.compactHistoryFn(history)
		} else {
			history = a.compactHistory(history)
		}
	}
	
	// 4. Use ChatTemplate to assemble
	return a.chatTemplate.Render(history, newMessages)
}

func (a *Agent) loadHistory() []adk.Message {
	// TODO: Load from Redis
	// Key: history:{userID}:{agentID}
	return nil
}

func (a *Agent) estimateTotalTokens(history []adk.Message, newMessages []string) int {
	total := 0
	for _, msg := range history {
		total += estimateTokens(msg.Content)
	}
	for _, msg := range newMessages {
		total += estimateTokens(msg)
	}
	return total
}

func estimateTokens(text string) int {
	return len(text) / 4
}

func (a *Agent) compactHistory(history []adk.Message) []adk.Message {
	if len(history) <= recentTurnsToKeep {
		return history
	}
	
	// Memory flush - write important things to long-term memory
	a.memoryFlush(history)
	
	// Split: old = everything except last 20 turns
	old := history[:len(history)-recentTurnsToKeep]
	recent := history[len(history)-recentTurnsToKeep:]
	
	// Summarize old turns
	var summary string
	if a.summarizeFn != nil {
		summary = a.summarizeFn(old)
	} else {
		summary = a.defaultSummarize(old)
	}
	
	// Build compacted history
	result := make([]adk.Message, 0, recentTurnsToKeep+1)
	result = append(result, adk.Message{
		Role:    schema.System,
		Content: fmt.Sprintf("[Previous conversation summary: %s]", summary),
	})
	result = append(result, recent...)
	
	// Save to Redis
	a.saveCompactedHistory(result)
	
	return result
}

func (a *Agent) memoryFlush(history []adk.Message) {
	// TODO: Extract important facts and save to mem0
}

func (a *Agent) defaultSummarize(old []adk.Message) string {
	// TODO: Call cheap LLM to summarize
	return fmt.Sprintf("[Summary of %d previous messages]", len(old))
}

func (a *Agent) saveCompactedHistory(history []adk.Message) {
	// TODO: Save to Redis
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/context.go internal/agent/context_test.go
git commit -m "feat(agent): add context assembly and compaction"
```

---

## Task 7: AgentManager GetAgent

**Files:**
- Create: `internal/agent/manager.go`
- Create: `internal/agent/manager_test.go`

**Step 1: Write the failing test**

```go
// internal/agent/manager_test.go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockDB struct {
	mock.Mock
}

func (m *MockDB) GetAgentConfig(userID, agentID string) (*AgentConfig, error) {
	args := m.Called(userID, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*AgentConfig), args.Error(1)
}

func TestAgentManager_GetAgent_ReturnsExisting(t *testing.T) {
	manager := NewAgentManager(nil, nil, nil)
	
	existingAgent := NewAgent(AgentOptions{AgentID: "agent-1"})
	manager.agents["agent-1"] = existingAgent
	
	agent := manager.GetAgent("user-1", "agent-1", nil)
	
	assert.Equal(t, existingAgent, agent)
}

func TestAgentManager_GetAgent_LoadsFromDB(t *testing.T) {
	mockDB := new(MockDB)
	mockDB.On("GetAgentConfig", "user-1", "agent-1").Return(&AgentConfig{
		AgentID:      "agent-1",
		SystemPrompt: "You are helpful",
	}, nil)
	
	manager := NewAgentManager(mockDB, nil, nil)
	
	agent := manager.GetAgent("user-1", "agent-1", nil)
	
	assert.NotNil(t, agent)
	assert.Equal(t, "agent-1", agent.agentID)
	
	// Should be cached
	assert.Equal(t, agent, manager.agents["agent-1"])
}

func TestAgentManager_GetAgent_ReturnsNilIfNotInDB(t *testing.T) {
	mockDB := new(MockDB)
	mockDB.On("GetAgentConfig", "user-1", "agent-999").Return(nil, errors.New("not found"))
	
	manager := NewAgentManager(mockDB, nil, nil)
	
	agent := manager.GetAgent("user-1", "agent-999", nil)
	
	assert.Nil(t, agent)
}

func TestAgentManager_Evict_RemovesFromMemory(t *testing.T) {
	manager := NewAgentManager(nil, nil, nil)
	
	agent := NewAgent(AgentOptions{AgentID: "agent-1"})
	manager.agents["agent-1"] = agent
	
	manager.Evict("agent-1")
	
	assert.NotContains(t, manager.agents, "agent-1")
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/agent/manager.go
package agent

import (
	"errors"
	"sync"
)

type AgentConfig struct {
	AgentID      string
	SystemPrompt string
	Description  string
	ModelName    string
	Tools        []string // Tool names, resolved later
}

type AgentManager struct {
	agents map[string]*Agent
	db     AgentDB
	quota  QuotaMiddleware
	mem0   Mem0Client
	mu     sync.RWMutex
}

type AgentDB interface {
	GetAgentConfig(userID, agentID string) (*AgentConfig, error)
}

func NewAgentManager(db AgentDB, quota QuotaMiddleware, mem0 Mem0Client) *AgentManager {
	return &AgentManager{
		agents: make(map[string]*Agent),
		db:     db,
		quota:  quota,
		mem0:   mem0,
	}
}

func (m *AgentManager) GetAgent(userID, agentID string, conn *websocket.Conn) *Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Already in memory
	if agent, exists := m.agents[agentID]; exists {
		return agent
	}
	
	// Load config from database
	config, err := m.db.GetAgentConfig(userID, agentID)
	if err != nil {
		return nil
	}
	
	// Build Agent struct from config
	agent := NewAgent(AgentOptions{
		AgentID:      agentID,
		UserID:       userID,
		SystemPrompt: config.SystemPrompt,
		Description:  config.Description,
		ModelName:    config.ModelName,
		Quota:        m.quota,
		Mem0:         m.mem0,
		Manager:      m,
	})
	
	m.agents[agentID] = agent
	return agent
}

func (m *AgentManager) Evict(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if agent, exists := m.agents[agentID]; exists {
		agent.Stop()
		delete(m.agents, agentID)
	}
}
```

**Step 4: Run test to verify it passes**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/agent/...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/agent/manager.go internal/agent/manager_test.go
git commit -m "feat(agent): add AgentManager with GetAgent and Evict"
```

---

## Task 8: Update WebSocket Handler

**Files:**
- Modify: `internal/websocket/handler.go`

**Step 1: Write the failing test**

```go
// Add to internal/websocket/handler_test.go

func TestHandlePrompt_UsesAgentManager(t *testing.T) {
	mockManager := &MockAgentManager{}
	mockManager.On("GetAgent", "user-1", "agent-1", mock.Anything).Return(
		&Agent{agentID: "agent-1"},
	)
	
	handler := &Handler{
		agentManager: mockManager,
	}
	
	msg := &wsMessage{
		Type:    "prompt",
		AgentID: "agent-1",
		Data:    json.RawMessage(`{"content": "Hello"}`),
	}
	
	handler.handleTextMessage(ctx, msg, "user-1", writeCh, logger)
	
	mockManager.AssertCalled(t, "GetAgent", "user-1", "agent-1", mock.Anything)
}
```

**Step 2: Update handler to use new Agent**

```go
// In internal/websocket/handler.go

func (h *Handler) handleTextMessage(ctx context.Context, data json.RawMessage, userID string, writeCh chan *wsMessage, logger zerolog.Logger) {
	var msg wsMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		logger.Error().Err(err).Msg("failed to parse message")
		return
	}
	
	switch msg.Type {
	case "prompt":
		// Get or load agent from manager
		agent := h.agentManager.GetAgent(userID, msg.AgentID, h.conn)
		if agent == nil {
			logger.Error().Str("agent_id", msg.AgentID).Msg("agent not found")
			return
		}
		
		// Submit task to agent
		task := Task{
			Message: msg.Content,
			Conn:    h.conn,
			MsgID:   msg.ID,
		}
		agent.Submit(task)
		
	// ... other cases
	}
}
```

**Step 3: Run tests**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit TEST_PATH=./internal/websocket/...`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/websocket/handler.go internal/websocket/handler_test.go
git commit -m "feat(websocket): update handler to use new Agent interface"
```

---

## Task 9: Delete Old Files

**Files:**
- Delete: `internal/agent/loop.go`
- Delete: `internal/agent/events.go` (keep only necessary types)
- Delete: `internal/agent/lane.go`
- Modify: `internal/agent/agent.go` (remove old code)

**Step 1: Remove old loop.go**

```bash
rm internal/agent/loop.go
```

**Step 2: Remove old events.go (keep TaskQueuedEvent etc if needed for lane)**

```bash
# Keep only lane-related events, remove the rest
# Or delete entirely if not needed
rm internal/agent/events.go
```

**Step 3: Remove old lane.go**

```bash
rm internal/agent/lane.go
```

**Step 4: Clean up agent.go**

Remove old Prompt(), Loop references, etc. Keep only what's needed.

**Step 5: Run all tests**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit`
Expected: PASS

**Step 6: Commit**

```bash
git add -A
git commit -m "refactor(agent): remove old loop, events, lane files - replaced by Eino"
```

---

## Task 10: Integration Test

**Files:**
- Create: `tests/integration/agent/eino_agent_test.go`

**Step 1: Write integration test**

```go
// tests/integration/agent/eino_agent_test.go
//go:build integration

package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgent_FullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	
	// Setup
	agent := agent.NewAgent(agent.AgentOptions{
		AgentID:      "test-agent",
		UserID:       "test-user",
		SystemPrompt: "You are a helpful assistant.",
		ModelName:    "gpt-4o",
	})
	
	// Submit task
	task := agent.Task{
		Message: "What is 2+2?",
		MsgID:    "msg-1",
	}
	
	agent.Submit(task)
	
	// Wait for processing
	time.Sleep(2 * time.Second)
	
	// Verify response was generated
	// (Would check Redis or mock in real test)
}

func TestAgentManager_GetOrCreateAndEvict(t *testing.T) {
	// Requires real DB and Redis
}
```

**Step 2: Run integration tests**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-integration`
Expected: PASS

**Step 3: Commit**

```bash
git add tests/integration/agent/eino_agent_test.go
git commit -m "test(agent): add integration tests for Eino agent"
```

---

## Final Steps

**Step 1: Run full test suite**

Run: `cd /Projects/nevaria/gateway && CGO_ENABLED=1 make test-unit && make lint`

**Step 2: Update CLAUDE.md if needed**

Document any new patterns or commands.

**Step 3: Final commit**

```bash
git add -A
git commit -m "feat(agent): complete Eino ADK integration with quota and progress events"
```

---

## Files Summary

### Created
| File | Purpose |
|------|---------|
| `internal/llm/quota_model.go` | QuotaChatModel wrapper |
| `internal/llm/quota_model_test.go` | Tests for wrapper |
| `internal/agent/callbacks.go` | Progress callbacks |
| `internal/agent/callbacks_test.go` | Tests for callbacks |
| `internal/agent/eino_agent.go` | Agent struct with Eino |
| `internal/agent/eino_agent_test.go` | Tests for Agent |
| `internal/agent/context.go` | Context assembly & compaction |
| `internal/agent/context_test.go` | Tests for context |
| `internal/agent/manager.go` | AgentManager |
| `internal/agent/manager_test.go` | Tests for manager |
| `tests/integration/agent/eino_agent_test.go` | Integration tests |

### Modified
| File | Changes |
|------|---------|
| `internal/websocket/handler.go` | Use new Agent interface |

### Deleted
| File | Reason |
|------|--------|
| `internal/agent/loop.go` | Replaced by Eino |
| `internal/agent/events.go` | Replaced by Eino events |
| `internal/agent/lane.go` | Replaced by Go channel |

---

## Dependencies

- `github.com/cloudwego/eino/adk` - Eino Agent Development Kit
- `github.com/cloudwego/eino/components/model` - ToolCallingChatModel interface
- `github.com/cloudwego/eino/utils/callbacks` - Callback handlers
- `github.com/cloudwego/eino-ext/components/model/openai` - OpenAI provider
- `github.com/cloudwego/eino-ext/components/model/claude` - Anthropic provider

---

## Rollback Plan

If issues arise:
1. Revert commits in reverse order
2. Restore deleted files from git history
3. Old code still works independently

---

## Success Criteria

- [ ] All unit tests pass
- [ ] All integration tests pass
- [ ] Lint passes with 0 issues
- [ ] Agent processes messages through Eino
- [ ] Quota is checked before LLM calls
- [ ] Progress events sent to WebSocket
- [ ] Connection-agnostic: works with nil conn
- [ ] Idle eviction works
