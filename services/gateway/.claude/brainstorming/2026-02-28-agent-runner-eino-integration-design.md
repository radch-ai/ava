# Agent Runner & Eino Integration Design

**Date:** 2026-02-28
**Status:** Design Complete - Ready for Implementation Plan

---

## Overview

Replace custom agent runner and loop with Eino ADK's `ChatModelAgent` and `Runner`. Implement quota integration via wrapper pattern.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Agent Struct                               │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  Fields:                                                             │
│  ├── id, userID, agentID                                             │
│  ├── ctx, cancel                                                     │
│  ├── laneQueue        chan Task          # Go channel for messages  │
│  ├── einoAgent        *adk.ChatModelAgent  # Built once at creation │
│  ├── einoRunner       *adk.Runner           # Built once at creation│
│  ├── manager          *AgentManager         # For eviction          │
│  ├── goroutineStarted bool                   # Lazy start flag      │
│  └── mu               sync.Mutex                                    │
│                                                                      │
│  Methods:                                                            │
│  ├── Submit(task)         # Add to lane, lazy start goroutine       │
│  ├── runLoop()            # Goroutine that processes lane           │
│  ├── runTurn(conn, input) # Single turn execution                   │
│  ├── assembleContext()    # Build messages for runner               │
│  ├── compactHistory()     # Context compaction                      │
│  └── Stop()               # Cancel context, cleanup                 │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        runLoop Goroutine                             │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  go func() {                                                         │
│      idleTimer := time.NewTimer(idleTimeout)                        │
│      defer idleTimer.Stop()                                          │
│                                                                      │
│      for {                                                           │
│          select {                                                    │
│          case task := <-a.laneQueue:                                │
│              idleTimer.Reset(idleTimeout)                           │
│              messages := drainBuffered(task)                        │
│              input := a.assembleContext(messages)                   │
│              a.runTurn(conn, input)                                 │
│                                                                      │
│          case <-idleTimer.C:                                        │
│              a.manager.Evict(a.id)                                  │
│              return                                                  │
│                                                                      │
│          case <-a.ctx.Done():                                       │
│              return                                                  │
│          }                                                           │
│      }                                                               │
│  }()                                                                 │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Key Design Decisions

### 1. Eino Components Used

| Component | Purpose | Created When |
|-----------|---------|--------------|
| `adk.ChatModelAgent` | ReAct loop logic | Once at NewAgent() |
| `adk.Runner` | Lifecycle management | Once at NewAgent() |
| `QuotaChatModel` | Wraps ToolCallingChatModel | Once at NewAgent() |

```go
func NewAgent(opts AgentOptions) *Agent {
    // 1. Create wrapped model with quota
    innerModel := factory.CreateChatModel(ctx, modelName)
    quotaModel := &QuotaChatModel{
        inner:   innerModel,
        quota:   opts.QuotaMiddleware,
        session: opts.QuotaSession,
    }
    
    // 2. Create Eino ChatModelAgent
    einoAgent, _ := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
        Name:        opts.AgentID,
        Description: opts.Description,
        Instruction: opts.SystemPrompt,
        Model:       quotaModel,  // Wrapped model
        ToolsConfig: adk.ToolsConfig{
            ToolsNodeConfig: compose.ToolsNodeConfig{
                Tools: opts.Tools,
            },
        },
    })
    
    // 3. Create Eino Runner
    einoRunner := adk.NewRunner(ctx, adk.RunnerConfig{
        Agent:           einoAgent,
        EnableStreaming: true,
    })
    
    return &Agent{
        einoAgent:  einoAgent,
        einoRunner: einoRunner,
        laneQueue:  make(chan Task, 100),
        // ...
    }
}
```

### 2. QuotaChatModel Wrapper

Implements `model.ToolCallingChatModel` interface:

```go
type QuotaChatModel struct {
    inner   model.ToolCallingChatModel
    quota   *QuotaMiddleware
    session *QuotaSession
}

// Generate - non-streaming
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

// Stream - streaming
func (q *QuotaChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
    if err := q.quota.BeforeLLM(ctx, q.session); err != nil {
        return nil, err
    }
    
    stream, err := q.inner.Stream(ctx, input, opts...)
    if err != nil {
        return nil, err
    }
    
    return q.wrapStreamForQuota(stream), nil
}

// WithTools - returns new wrapped instance
func (q *QuotaChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
    innerWithTools, err := q.inner.WithTools(tools)
    if err != nil {
        return nil, err
    }
    return &QuotaChatModel{inner: innerWithTools, quota: q.quota, session: q.session}, nil
}
```

### 3. Lane Queue - Go Channel

Replace `Lane` struct with simple buffered channel:

```go
type Task struct {
    Message string
    Conn    *websocket.Conn  // Can be nil for background tasks
    MsgID   string
}

laneQueue chan Task  // make(chan Task, 100)
```

### 4. Lazy Goroutine Start

```go
func (a *Agent) Submit(task Task) {
    select {
    case a.laneQueue <- task:
        // Task queued
    default:
        // Queue full, handle error
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
```

### 5. runTurn - Connection Agnostic

```go
func (a *Agent) runTurn(conn *websocket.Conn, input string) {
    // Phase 1: Stream to client (best effort)
    var buffer strings.Builder
    
    // Build progress callback handler with conn reference
    progressHandler := a.buildProgressHandler(conn)
    
    iter := a.einoRunner.Query(a.ctx, input, compose.WithCallbacks(progressHandler))
    for {
        event, ok := iter.Next()
        if !ok {
            break
        }
        
        if event.Err != nil {
            // Handle error with retry logic
            break
        }
        
        // Accumulate regardless of conn state
        if event.Output != nil {
            msg, _ := event.Output.MessageOutput.GetMessage()
            buffer.WriteString(msg.Content)
        }
        
        // Try to write to conn
        if conn != nil {
            err := conn.WriteJSON(event)
            if err != nil {
                conn = nil  // Mark dead, stop trying
            }
        }
    }
    
    // Phase 2: Always happens (conn alive or dead)
    a.saveToRedis(buffer.String())
    
    if conn == nil {
        a.sendNotification()
    }
}
```

### 6. System Prompt & Memory (mem0)

Built at Agent creation time (before goroutine starts), not inside goroutine:

```go
func NewAgent(opts AgentOptions) *Agent {
    // 1. Build system prompt with mem0 memory at top
    systemPrompt := a.buildSystemPrompt(opts)
    
    // 2. Create Eino ChatModelAgent with system prompt as Instruction
    einoAgent, _ := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
        Name:        opts.AgentID,
        Description: opts.Description,
        Instruction: systemPrompt,  // System prompt + mem0 here
        Model:       quotaModel,
        ToolsConfig: adk.ToolsConfig{...},
    })
    
    // ... rest of initialization
}

func (a *Agent) buildSystemPrompt(opts AgentOptions) string {
    // Base system prompt from config
    basePrompt := opts.SystemPrompt
    
    // Load mem0 memories (user preferences, facts, etc.)
    memories := a.mem0Client.GetMemories(a.userID, a.agentID)
    
    // Format: system prompt + mem0 at top of history
    var sb strings.Builder
    sb.WriteString(basePrompt)
    sb.WriteString("\n\n")
    sb.WriteString("## User Context (memories)\n")
    for _, mem := range memories {
        sb.WriteString(fmt.Sprintf("- %s\n", mem.Content))
    }
    
    return sb.String()
}
```

**mem0 Structure:**
- Stored per (userID, agentID)
- Loaded once at agent creation
- Placed in system prompt (top of conversation)
- Updated via mem0 API when new memories are created

### 7. Context Assembly & Compaction

Before each runTurn (conversation history, not system prompt):
```go
func (a *Agent) assembleContext(newMessages []string) []adk.Message {
    // 1. Load conversation history from Redis (NOT system prompt - that's in Instruction)
    history := a.loadHistory()
    
    // 2. Estimate tokens (chars/4 heuristic)
    estimatedTokens := estimateTokens(history, newMessages)
    
    // 3. If over 70% threshold, compact
    if estimatedTokens > a.contextLimit * 0.7 {
        a.compactHistory(history)
    }
    
    // 4. Use ChatTemplate to assemble (system prompt already in agent's Instruction)
    return a.chatTemplate.Render(history, newMessages)
}

func (a *Agent) compactHistory(history []adk.Message) {
    // a. Memory flush - write important things to long-term memory
    a.memoryFlush(history)
    
    // b. Split: old = everything except last 20 turns
    old, recent := splitHistory(history, 20)
    
    // c. Summarize old turns with cheap model
    summary := a.summarize(old)
    
    // d. Replace old with summary in Redis
    a.saveCompactedHistory(summary, recent)
}
```

On `context_length_exceeded` error:
- Compact → Retry (up to 3x)

### 8. Model Selection Strategy

| Session Type | Selection Frequency |
|--------------|---------------------|
| WebSocket (PA Agent) | Every 15-30 min OR every N turns |
| WebSocket (Worker) | Every 15-30 min OR every N turns |
| Cron/Heartbeat | Once per session |
| Agent-to-agent | Once per session |
| User-specified | Fixed until changed |

Selection uses Not-Diamond with Redis caching (already implemented).

### 9. History Storage

- **Active history:** Redis (fast access, TTL-based)
- **Long-term archive:** PostgreSQL (persistent, queryable)

### 10. Agent Types & WebSocket Patterns

```
PA Agent (Personal Assistant):
└── WebSocket always open while app is active
    └── User frequently here
    └── Notifications, streaming, everything

Worker Agents:
└── WebSocket opened only when user navigates to them
    └── Rare, short sessions
    └── Closes when user leaves
    └── Most of time: no WebSocket
    └── Results via notification when backgrounded
```

### 11. Error Handling

| Error Type | Action |
|------------|--------|
| Transient (network, timeout) | Retry with exponential backoff |
| Quota exhausted | Fail immediately, notify user |
| Auth error | Fail immediately |
| Context length exceeded | Compact, retry up to 3x |

### 12. Agent Manager

**Key distinction:**
- **Create agent (DB only)** - Separate operation, just stores config/metadata in database
- **GetAgent (in-memory)** - Loads config from DB and creates Agent struct only when user sends first message

```go
type AgentManager struct {
    agents    map[string]*Agent  // agentID -> Agent (in-memory only)
    db        *pgxpool.Pool
    redis     *redis.Client
    quota     *QuotaMiddleware
    mem0      *Mem0Client
    mu        sync.RWMutex
}

// GetAgent returns in-memory agent, or creates it from DB config
// Does NOT create DB entry - that's a separate operation
func (m *AgentManager) GetAgent(userID, agentID string, conn *websocket.Conn) *Agent {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    // Already in memory
    if agent, exists := m.agents[agentID]; exists {
        return agent
    }
    
    // Load config from database (agent was created separately via API/cron/etc)
    config, err := m.loadAgentConfig(userID, agentID)
    if err != nil {
        // Agent doesn't exist in DB - error, not auto-create
        return nil
    }
    
    // Build Agent struct from config
    agent := NewAgent(AgentOptions{
        AgentID:   agentID,
        UserID:    userID,
        Config:    config,
        Quota:     m.quota,
        Mem0:      m.mem0,
        Manager:   m,
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

// CreateAgentInDB - separate operation, just DB entry
// Called via API, cron, or other service
func (m *AgentManager) CreateAgentInDB(userID string, config AgentConfig) error {
    // Just insert into database, no in-memory Agent created yet
    return m.db.InsertAgent(ctx, userID, config)
}
```

**Flow:**
```
1. API call: CreateAgentInDB() → Just DB entry (name, system_prompt, tools config, etc.)
2. User sends first message:
   - GetAgent(agentID) → Load config from DB → Create Agent struct → Store in memory
   - Submit(task) → Lazy start goroutine
3. User continues using agent → Already in memory, GetAgent returns it directly
4. Idle timeout → Evict from memory (DB entry still exists)
5. User returns → GetAgent → Load from DB again → Create Agent struct
```

### 13. Tools

- Fixed at agent creation time
- Passed to `ChatModelAgentConfig.ToolsConfig`
- Tool execution handled by Eino internally

### 14. Progress Events via Callbacks

**Problem:** Eino buffers internally during tool decision, causing silence.

**Solution:** Register callbacks to emit progress events during ReAct loop:

```go
func (a *Agent) buildProgressHandler(conn *websocket.Conn) callbacks.Handler {
    emitToWS := func(eventType string, data map[string]any) {
        if conn != nil {
            conn.WriteJSON(map[string]any{
                "type": eventType,
                "data": data,
            })
        }
    }
    
    return callbacks.NewHandlerHelper().
        ChatModel(&callbacks.ModelCallbackHandler{
            OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *model.CallbackInput) context.Context {
                emitToWS("thinking", nil)
                return ctx
            },
        }).
        Tool(&callbacks.ToolCallbackHandler{
            OnStart: func(ctx context.Context, info *callbacks.RunInfo, input *tool.CallbackInput) context.Context {
                emitToWS("tool_start", map[string]any{
                    "tool_name": info.ComponentName,
                    "args":      input.ArgumentsInJSON,
                })
                return ctx
            },
            OnEnd: func(ctx context.Context, info *callbacks.RunInfo, output *tool.CallbackOutput) context.Context {
                emitToWS("tool_end", map[string]any{
                    "tool_name": info.ComponentName,
                    "result":    output.Response,
                })
                return ctx
            },
        }).
        Handler()
}
```

**WebSocket Event Types:**

| Event | When | Payload |
|-------|------|---------|
| `thinking` | LLM starts generation | `{}` |
| `tool_start` | Tool execution starts | `{tool_name, args}` |
| `tool_end` | Tool execution ends | `{tool_name, result}` |
| `eino_event` | Final answer streaming | Raw Eino AgentEvent |

**Resulting UX:**
```
User: "Book me a flight"
    ↓
WS: {"type": "thinking"}
    ↓
WS: {"type": "tool_start", "tool_name": "search_flights", "args": "..."}
    ↓
WS: {"type": "tool_end", "tool_name": "search_flights", "result": "..."}
    ↓
WS: {"type": "eino_event", ...} (streamed tokens for final answer)
```

**Why this works:**
- Eino buffers during tool decision (can't avoid)
- Callbacks fire at exact moments (tool start/end)
- User sees progress instead of silence
- Feels like a responsive assistant

**Files to Create:**
| File | Purpose |
|------|---------|
| `internal/agent/callbacks.go` | Callback handlers for progress events |

---

## Files to Create/Modify

### Create
| File | Purpose |
|------|---------|
| `internal/agent/eino_agent.go` | Agent struct with Eino integration |
| `internal/agent/eino_runner.go` | Runner goroutine logic |
| `internal/llm/quota_model.go` | QuotaChatModel wrapper |
| `internal/agent/context.go` | Context assembly & compaction |
| `internal/agent/callbacks.go` | Callback handlers for progress events |

### Modify
| File | Changes |
|------|---------|
| `internal/agent/agent.go` | Remove old Loop, add Eino fields |
| `internal/agent/loop.go` | DELETE - replaced by Eino |
| `internal/agent/events.go` | DELETE most events - use Eino events |
| `internal/agent/lane.go` | Replace with channel-based queue |
| `internal/websocket/handler.go` | Update to use new Agent interface |

---

## Deferred for Later

1. **Checkpointing/Resume** - Eino ADK feature, implement when needed
2. **Agent-to-agent transfer** - Eino ADK feature, implement when needed
3. **Interrupt/resume** - Human-in-the-loop, implement when needed

---

## Integration Points

```
WebSocket Handler
    │
    ▼
AgentManager.GetOrCreate(userID, agentID, conn)
    │
    ▼
Agent.Submit(Task{msg, conn, msgID})
    │
    ▼
laneQueue channel
    │
    ▼
runLoop goroutine
    │
    ▼
runTurn(conn, input)
    │
    ├── assembleContext() → ChatTemplate → Messages
    │
    ▼
einoRunner.Query(ctx, input, compose.WithCallbacks(progressHandler))
    │
    ▼
ChatModelAgent (ReAct loop)
    │
    ├── [thinking...] ← ModelCallbackHandler.OnStart
    │
    ├── QuotaChatModel.Generate/Stream
    │       │
    │       ├── BeforeLLM (quota check)
    │       ├── OpenAI/Anthropic API
    │       └── AfterLLM (deduct cost)
    │
    ├── [calling tool: X] ← ToolCallbackHandler.OnStart
    ├── Tool execution
    ├── [tool result received] ← ToolCallbackHandler.OnEnd
    │
    └── Final answer tokens (streamed via AgentEvent)
    │
    ▼
Events streamed to WebSocket (or buffered if conn dead)
    │
    ▼
Save to Redis → Notify if needed
```

---

## Summary

| Aspect | Decision |
|--------|----------|
| Agent runtime | Eino `ChatModelAgent` + `Runner` |
| Quota integration | `QuotaChatModel` wrapper |
| Lane queue | Go channel (buffered) |
| Goroutine start | Lazy on first Submit |
| Goroutine stop | Idle timeout or ctx.Done |
| Events | Eino `AgentEvent` + callback progress events |
| Progress events | `thinking`, `tool_start`, `tool_end` via callbacks |
| Conn handling | Connection-agnostic, buffer always |
| History | Redis (active) + PostgreSQL (archive) |
| Compaction | At 70% threshold, summarize old turns |
| Model selection | Not-Diamond with tier-based caching |
| Tools | Fixed at creation |
| Notifications | FCM/APNs via existing service |

---

## Next Steps

1. Write implementation plan
2. Implement `QuotaChatModel` wrapper
3. Implement new `Agent` struct with Eino
4. Implement progress callbacks
5. Update `AgentManager`
6. Update WebSocket handler
7. Add tests
8. Remove old `loop.go`, `events.go`, `lane.go`
