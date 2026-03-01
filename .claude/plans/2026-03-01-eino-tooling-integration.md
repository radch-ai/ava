# Eino Tooling Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete Eino ADK integration with proper tool support.

**Architecture:**
- Agent struct holds einoAgent (adk.Agent) and einoRunner (*adk.Runner)
- HTTP/2 session handler creates agent, coordinates via lock
- Agent independent, survives socket disconnect
- WSConnRef for swappable websocket connection
- Global tools: DuckDuckGo search from eino-ext
- Mem0 handled automatically via ChatTemplate, not as tools

**Tech Stack:** Go, Eino ADK, gorilla/websocket, Redis, mem0

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                   HTTP/2 Session Handler                        │
│                                                                 │
│  On message received:                                           │
│  1. mu.Lock()                                                   │
│  2. if agent == nil:                                            │
│        agent = NewAgent(firstMsg)  // creates laneCh, adds msg  │
│        mu.Unlock()                                              │
│        agent.Start()               // sync init + goroutine     │
│        return                                                   │
│     else:                                                       │
│        agent.laneCh <- task                                     │
│        mu.Unlock()                                              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                     Agent (Independent)                         │
│                                                                 │
│  - laneCh chan Task                                             │
│  - mu sync.Mutex (shared with session handler)                  │
│  - connRef *WSConnRef (swappable)                               │
│  - einoAgent adk.Agent                                          │
│  - einoRunner *adk.Runner                                       │
│                                                                 │
│  Loop:                                                          │
│    select {                                                     │
│    case task := <-laneCh:                                       │
│        // Drain all messages                                    │
│        // runner.Query (full ReAct loop)                        │
│        // Stream to connRef.Get()                               │
│        // Save to session, notify if disconnected               │
│                                                                 │
│    case <-idleTimer.C:                                          │
│        mu.Lock()                                                │
│        if len(laneCh) == 0 { return }                           │
│        mu.Unlock()                                              │
│    }                                                            │
└─────────────────────────────────────────────────────────────────┘
```

## Mem0 Handling (Automatic)

```
Session start:
  message[0]: mem0 memories injected (primacy)

Pre-compaction:
  1. Strip message[0] memories
  2. Call mem0 API to store from raw conversation
  3. Compact clean conversation with dedicated system prompt

Post-compaction:
  message[0]: mem0 memories (re-injected)
  [compacted summary]
  [recent history]
  [latest user message]
```

---

## Task 1: WSConnRef - Swappable Connection

**Files:**
- Create: `internal/agent/conn_ref.go`
- Create: `internal/agent/conn_ref_test.go`

**Step 1: Write failing test**

```go
// internal/agent/conn_ref_test.go
package agent

import (
    "testing"
    
    "github.com/gorilla/websocket"
    "github.com/stretchr/testify/assert"
)

func TestWSConnRef_GetSetClear(t *testing.T) {
    ref := NewWSConnRef()
    
    assert.Nil(t, ref.Get())
    assert.False(t, ref.IsSet())
    
    mockConn := &websocket.Conn{}
    ref.Set(mockConn)
    
    assert.Equal(t, mockConn, ref.Get())
    assert.True(t, ref.IsSet())
    
    ref.Clear()
    
    assert.Nil(t, ref.Get())
    assert.False(t, ref.IsSet())
}

func TestWSConnRef_Concurrent(t *testing.T) {
    ref := NewWSConnRef()
    
    done := make(chan bool)
    
    go func() {
        for i := 0; i < 100; i++ {
            ref.Set(nil)
        }
        done <- true
    }()
    
    go func() {
        for i := 0; i < 100; i++ {
            _ = ref.Get()
        }
        done <- true
    }()
    
    <-done
    <-done
}
```

**Step 2: Run test, verify FAIL**

**Step 3: Implement**

```go
// internal/agent/conn_ref.go
package agent

import (
    "sync"

    "github.com/gorilla/websocket"
)

type WSConnRef struct {
    mu   sync.RWMutex
    conn *websocket.Conn
}

func NewWSConnRef() *WSConnRef {
    return &WSConnRef{}
}

func (r *WSConnRef) Get() *websocket.Conn {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.conn
}

func (r *WSConnRef) Set(conn *websocket.Conn) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.conn = conn
}

func (r *WSConnRef) Clear() {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.conn = nil
}

func (r *WSConnRef) IsSet() bool {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.conn != nil
}
```

**Step 4: Run test, verify PASS**

**Step 5: Commit**

```bash
git add internal/agent/conn_ref.go internal/agent/conn_ref_test.go
git commit -m "feat(agent): add WSConnRef for swappable websocket connections"
```

---

## Task 2: Agent Struct with Eino Components

**Files:**
- Modify: `internal/agent/agent.go` (or create if needed)
- Modify: `internal/agent/agent_test.go`

**Step 1: Write failing test**

```go
// internal/agent/agent_test.go
package agent

import (
    "context"
    "testing"
    
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestNewAgent_CreatesLaneCh(t *testing.T) {
    ctx := context.Background()
    task := Task{Message: "hello", MsgID: "msg1"}
    opts := AgentOptions{
        AgentID: "agent1",
        UserID:  "user1",
    }
    
    agent := NewAgent(ctx, task, opts)
    
    assert.NotNil(t, agent)
    assert.NotNil(t, agent.laneCh)
    assert.Equal(t, "agent1", agent.agentID)
    assert.Equal(t, "user1", agent.userID)
    assert.NotNil(t, agent.connRef)
}

func TestNewAgent_QueuesFirstMessage(t *testing.T) {
    ctx := context.Background()
    task := Task{Message: "hello", MsgID: "msg1"}
    
    agent := NewAgent(ctx, task, AgentOptions{})
    
    // First message should be in channel
    assert.Equal(t, 1, len(agent.laneCh))
}

func TestAgent_Start_InitializesEino(t *testing.T) {
    ctx := context.Background()
    task := Task{Message: "hello"}
    
    agent := NewAgent(ctx, task, AgentOptions{
        AgentID:      "agent1",
        SystemPrompt: "You are helpful",
    })
    
    err := agent.Start(AgentOptions{
        QuotaChatModel: &MockQuotaChatModel{},
        Memories:       []string{},
    })
    
    require.NoError(t, err)
    assert.NotNil(t, agent.einoAgent)
    assert.NotNil(t, agent.einoRunner)
}
```

**Step 2: Run test, verify FAIL**

**Step 3: Implement**

```go
// internal/agent/agent.go
package agent

import (
    "context"
    "sync"

    "github.com/cloudwego/eino/compose"
    "github.com/cloudwego/eino/flow/adk"
)

type AgentOptions struct {
    AgentID       string
    UserID        string
    SystemPrompt  string
    QuotaChatModel QuotaChatModel
    Memories      []string
}

type Agent struct {
    agentID string
    userID  string
    ctx     context.Context
    cancel  context.CancelFunc
    
    laneCh chan Task
    mu     sync.Mutex
    
    connRef *WSConnRef
    
    einoAgent  adk.Agent
    einoRunner *adk.Runner
}

func NewAgent(ctx context.Context, firstTask Task, opts AgentOptions) *Agent {
    ctx, cancel := context.WithCancel(ctx)
    
    a := &Agent{
        agentID: opts.AgentID,
        userID:  opts.UserID,
        ctx:     ctx,
        cancel:  cancel,
        laneCh:  make(chan Task, 100),
        connRef: NewWSConnRef(),
    }
    
    a.laneCh <- firstTask
    
    return a
}

func (a *Agent) Start(opts AgentOptions) error {
    chatModel := opts.QuotaChatModel.GetInnerModel()
    
    tools := GetGlobalTools()
    template := buildChatTemplate(opts.SystemPrompt, opts.Memories)
    
    var err error
    a.einoAgent, err = adk.NewChatModelAgent(a.ctx, &adk.ChatModelAgentConfig{
        Model:        chatModel,
        ChatTemplate: template,
        ToolsConfig: compose.ToolsNodeConfig{
            Tools: tools,
        },
    })
    if err != nil {
        return err
    }
    
    a.einoRunner = adk.NewRunner(a.ctx, adk.RunnerConfig{
        Agent: a.einoAgent,
    })
    
    go a.loop()
    
    return nil
}

func (a *Agent) Stop() {
    if a.cancel != nil {
        a.cancel()
    }
}
```

**Step 4: Run test, verify PASS**

**Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/agent_test.go
git commit -m "feat(agent): add Agent struct with Eino ChatModelAgent and Runner"
```

---

## Task 3: Agent Loop with Message Coalescing

**Files:**
- Create: `internal/agent/loop.go`
- Create: `internal/agent/loop_test.go`

**Step 1: Write failing test**

```go
// internal/agent/loop_test.go
package agent

import (
    "context"
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
)

func TestAgent_Loop_DrainsMessages(t *testing.T) {
    ctx := context.Background()
    agent := NewAgent(ctx, Task{Message: "first"}, AgentOptions{})
    
    // Queue more messages before loop starts
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
    
    // Should have drained all messages
    assert.GreaterOrEqual(t, len(receivedMessages), 3)
}

func TestAgent_Loop_IdleExit(t *testing.T) {
    ctx := context.Background()
    agent := NewAgent(ctx, Task{Message: "first"}, AgentOptions{
        IdleTimeout: 50 * time.Millisecond,
    })
    
    exited := make(chan bool, 1)
    agent.onExit = func() {
        exited <- true
    }
    
    go agent.loop()
    
    select {
    case <-exited:
        // Good - exited on idle
    case <-time.After(200 * time.Millisecond):
        t.Fatal("expected agent to exit on idle")
    }
}
```

**Step 2: Run test, verify FAIL**

**Step 3: Implement**

```go
// internal/agent/loop.go
package agent

import (
    "time"
)

const defaultIdleTimeout = 5 * time.Minute

type Agent struct {
    // ... existing fields ...
    idleTimeout    time.Duration
    processTasksFn func([]Task) // for testing
    onExit         func()       // for testing
}

func (a *Agent) loop() {
    idleTimeout := a.idleTimeout
    if idleTimeout == 0 {
        idleTimeout = defaultIdleTimeout
    }
    
    idleTimer := time.NewTimer(idleTimeout)
    defer idleTimer.Stop()
    
    for {
        select {
        case task := <-a.laneCh:
            idleTimer.Reset(idleTimeout)
            
            tasks := a.drainMessages(task)
            
            if a.processTasksFn != nil {
                a.processTasksFn(tasks)
            } else {
                a.processTasks(tasks)
            }
            
        case <-idleTimer.C:
            a.mu.Lock()
            if len(a.laneCh) == 0 {
                a.mu.Unlock()
                if a.onExit != nil {
                    a.onExit()
                }
                return
            }
            a.mu.Unlock()
            idleTimer.Reset(idleTimeout)
            
        case <-a.ctx.Done():
            if a.onExit != nil {
                a.onExit()
            }
            return
        }
    }
}

func (a *Agent) drainMessages(first Task) []Task {
    tasks := []Task{first}
    
    for {
        select {
        case t := <-a.laneCh:
            tasks = append(tasks, t)
        default:
            return tasks
        }
    }
}
```

**Step 4: Run test, verify PASS**

**Step 5: Commit**

```bash
git add internal/agent/loop.go internal/agent/loop_test.go
git commit -m "feat(agent): implement loop with message coalescing and idle exit"
```

---

## Task 4: Process Tasks with Eino Query

**Files:**
- Modify: `internal/agent/loop.go`
- Create: `internal/agent/process_test.go`

**Step 1: Write failing test**

```go
// internal/agent/process_test.go
package agent

import (
    "context"
    "strings"
    "testing"
    
    "github.com/stretchr/testify/assert"
)

func TestAgent_ProcessTasks_CombinesInput(t *testing.T) {
    ctx := context.Background()
    agent := NewAgent(ctx, Task{}, AgentOptions{})
    
    var capturedInput string
    agent.einoRunner = &MockRunner{
        QueryFn: func(ctx context.Context, input string, opts ...interface{}) (string, error) {
            capturedInput = input
            return "response", nil
        },
    }
    
    tasks := []Task{
        {Message: "hello"},
        {Message: "world"},
    }
    
    agent.processTasks(tasks)
    
    assert.Contains(t, capturedInput, "hello")
    assert.Contains(t, capturedInput, "world")
}

func TestAgent_ProcessTasks_StreamsToConn(t *testing.T) {
    agent := NewAgent(context.Background(), Task{}, AgentOptions{})
    
    mockConn := &MockWSConn{}
    agent.connRef.Set(mockConn)
    agent.einoRunner = &MockRunner{
        QueryFn: func(ctx context.Context, input string, opts ...interface{}) (string, error) {
            return "response", nil
        },
    }
    
    agent.processTasks([]Task{{Message: "test"}})
    
    assert.True(t, len(mockConn.Written) > 0)
}

func TestAgent_ProcessTasks_SavesOnNilConn(t *testing.T) {
    agent := NewAgent(context.Background(), Task{}, AgentOptions{})
    agent.einoRunner = &MockRunner{
        QueryFn: func(ctx context.Context, input string, opts ...interface{}) (string, error) {
            return "response", nil
        },
    }
    
    var savedToSession string
    agent.saveToSessionFn = func(content string) {
        savedToSession = content
    }
    
    agent.processTasks([]Task{{Message: "test"}})
    
    assert.NotEmpty(t, savedToSession)
}
```

**Step 2: Run test, verify FAIL**

**Step 3: Implement**

```go
// Add to internal/agent/loop.go

import (
    "strings"
    
    "github.com/cloudwego/eino/compose"
)

func (a *Agent) processTasks(tasks []Task) {
    var input strings.Builder
    var msgID string
    
    for _, t := range tasks {
        if input.Len() > 0 {
            input.WriteString("\n")
        }
        input.WriteString(t.Message)
        if msgID == "" && t.MsgID != "" {
            msgID = t.MsgID
        }
    }
    
    handler := BuildProgressHandler(a.connRef.Get())
    
    result, err := a.einoRunner.Query(a.ctx, input.String(), compose.WithCallbacks(handler))
    if err != nil {
        // Handle error
        return
    }
    
    a.saveToSession(result)
    
    if !a.connRef.IsSet() {
        a.notifyUser(msgID, result)
    }
}

func (a *Agent) saveToSession(content string) {
    // TODO: Implement with session store
}

func (a *Agent) notifyUser(msgID, content string) {
    // TODO: Implement with notification service
}
```

**Step 4: Run test, verify PASS**

**Step 5: Commit**

```bash
git add internal/agent/loop.go internal/agent/process_test.go
git commit -m "feat(agent): implement processTasks with Eino query and streaming"
```

---

## Task 5: Global Tools

**Files:**
- Create: `internal/agent/tools.go`
- Create: `internal/agent/tools_test.go`

**Step 1: Write failing test**

```go
// internal/agent/tools_test.go
package agent

import (
    "testing"
    
    "github.com/stretchr/testify/assert"
)

func TestGetGlobalTools_ReturnsTools(t *testing.T) {
    tools := GetGlobalTools()
    
    assert.NotEmpty(t, tools)
    
    // Should have at least duckduckgo
    found := false
    for _, tool := range tools {
        info, _ := tool.Info(nil)
        if info.Name == "duckduckgo_search" || info.Name == "ddg_search" {
            found = true
        }
    }
    assert.True(t, found, "should include duckduckgo search tool")
}
```

**Step 2: Run test, verify FAIL**

**Step 3: Implement**

```go
// internal/agent/tools.go
package agent

import (
    "github.com/cloudwego/eino/components/tool"
    "github.com/cloudwego/eino-ext/components/tool/ddgsearch"
)

func GetGlobalTools() []tool.BaseTool {
    return []tool.BaseTool{
        ddgsearch.NewTool(),
    }
}
```

**Step 4: Run test, verify PASS**

**Step 5: Commit**

```bash
git add internal/agent/tools.go internal/agent/tools_test.go
git commit -m "feat(agent): add GetGlobalTools with DuckDuckGo search"
```

---

## Task 6: ChatTemplate with Mem0

**Files:**
- Create: `internal/agent/template.go`
- Create: `internal/agent/template_test.go`

**Step 1: Write failing test**

```go
// internal/agent/template_test.go
package agent

import (
    "testing"
    
    "github.com/stretchr/testify/assert"
)

func TestBuildChatTemplate_WithMemories(t *testing.T) {
    systemPrompt := "You are helpful."
    memories := []string{
        "User likes coffee",
        "User lives in NYC",
    }
    
    template := buildChatTemplate(systemPrompt, memories)
    
    assert.NotNil(t, template)
    // Verify memories are included as first message
}

func TestBuildChatTemplate_NoMemories(t *testing.T) {
    systemPrompt := "You are helpful."
    
    template := buildChatTemplate(systemPrompt, nil)
    
    assert.NotNil(t, template)
}
```

**Step 2: Run test, verify FAIL**

**Step 3: Implement**

```go
// internal/agent/template.go
package agent

import (
    "fmt"
    
    "github.com/cloudwego/eino/components/prompt"
    "github.com/cloudwego/eino/schema"
)

func buildChatTemplate(systemPrompt string, memories []string) prompt.ChatTemplate {
    messages := make([]*schema.Message, 0)
    
    // System prompt
    messages = append(messages, &schema.Message{
        Role:    schema.System,
        Content: systemPrompt,
    })
    
    // Memories as first user message (primacy)
    if len(memories) > 0 {
        var memContent string
        for _, mem := range memories {
            memContent += fmt.Sprintf("- %s\n", mem)
        }
        messages = append(messages, &schema.Message{
            Role:    schema.User,
            Content: fmt.Sprintf("Context about me:\n%s", memContent),
        })
    }
    
    return prompt.NewChatTemplate(&prompt.ChatTemplateConfig{
        Messages: messages,
    })
}
```

**Step 4: Run test, verify PASS**

**Step 5: Commit**

```bash
git add internal/agent/template.go internal/agent/template_test.go
git commit -m "feat(agent): add ChatTemplate builder with mem0 primacy"
```

---

## Task 7: Update Session Handler

**Files:**
- Modify: `internal/websocket/session.go` (or equivalent)

**Step 1: Update to create agent with lock pattern**

```go
// In session handler
func (h *Handler) handleMessage(ctx context.Context, userID, agentID, message string) {
    h.mu.Lock()
    
    agent := h.agents[agentID]
    if agent == nil {
        agent = NewAgent(ctx, Task{Message: message, MsgID: generateMsgID()}, AgentOptions{
            AgentID: agentID,
            UserID:  userID,
        })
        h.agents[agentID] = agent
        h.mu.Unlock()
        
        // Load config, start agent
        config := h.loadAgentConfig(userID, agentID)
        agent.Start(AgentOptions{
            SystemPrompt:   config.SystemPrompt,
            QuotaChatModel: h.quotaChatModel,
            Memories:       h.loadMemories(userID),
        })
    } else {
        agent.laneCh <- Task{Message: message, MsgID: generateMsgID()}
        h.mu.Unlock()
    }
}
```

**Step 2: Run tests**

**Step 3: Commit**

```bash
git add internal/websocket/session.go
git commit -m "feat(websocket): update session handler with agent creation pattern"
```

---

## Task 8: Cleanup Old Files

**Files to remove/modify:**
- Remove or update: `internal/agent/eino_agent.go` (old interface-based)
- Remove or update: `internal/agent/manager.go` (if not needed)
- Update: `internal/llm/eino_client.go` (simplify or remove)

**Step 1: Review and remove**

```bash
# Remove old interface-based implementations if replaced
# Keep what's still needed
```

**Step 2: Run all tests**

```bash
make test-unit
make lint
```

**Step 3: Commit**

```bash
git add -A
git commit -m "refactor(agent): cleanup old Eino interface implementations"
```

---

## Task 9: Integration Test

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
)

func TestAgent_FullFlow(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }
    
    ctx := context.Background()
    
    agent := agent.NewAgent(ctx, agent.Task{
        Message: "What is 2+2?",
        MsgID:   "msg-1",
    }, agent.AgentOptions{
        AgentID: "test-agent",
        UserID:  "test-user",
    })
    
    err := agent.Start(agent.AgentOptions{
        SystemPrompt:   "You are helpful.",
        QuotaChatModel: getTestQuotaModel(),
    })
    
    assert.NoError(t, err)
    
    // Wait for processing
    time.Sleep(5 * time.Second)
    
    agent.Stop()
}
```

**Step 2: Run integration tests**

```bash
make test-integration
```

**Step 3: Commit**

```bash
git add tests/integration/agent/eino_agent_test.go
git commit -m "test(agent): add integration test for Eino agent"
```

---

## Files Summary

### Created
| File | Purpose |
|------|---------|
| `internal/agent/conn_ref.go` | WSConnRef swappable connection |
| `internal/agent/loop.go` | Agent loop with coalescing |
| `internal/agent/tools.go` | GetGlobalTools |
| `internal/agent/template.go` | ChatTemplate builder |

### Modified
| File | Changes |
|------|---------|
| `internal/agent/agent.go` | Agent struct with einoAgent, einoRunner |
| `internal/websocket/session.go` | Lock pattern for agent creation |

### Removed
| File | Reason |
|------|--------|
| Old interface implementations | Replaced by real Eino ADK |

---

## Success Criteria

- [ ] All unit tests pass
- [ ] All integration tests pass
- [ ] Lint passes
- [ ] Agent processes messages through Eino
- [ ] Tools work (DuckDuckGo search)
- [ ] Progress events stream to WebSocket
- [ ] Connection-agnostic: works when disconnected
- [ ] Idle eviction works with lock coordination
