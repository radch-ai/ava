# Agent

Core agent runtime framework with state management, event streaming, and tool execution loop.

## Structs

### Agent

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Agent` | `mu` | `sync.RWMutex` | State mutex |
| | `state` | `AgentState` | Current agent state |
| | `id` | `string` | Unique agent instance ID |
| | `userID` | `string` | User identifier (multi-agent isolation) |
| | `agentID` | `string` | Agent identifier (multi-agent routing) |
| | `subscribers` | `map[string]chan AgentEvent` | Event subscribers |
| | `ctx` | `context.Context` | Agent context |
| | `cancel` | `context.CancelFunc` | Context cancel function |
| | `llmClient` | `LLMClient` | LLM client for tool calling |

### AgentState

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AgentState` | `SystemPrompt` | `string` | System prompt for LLM |
| | `Model` | `Model` | Model configuration |
| | `ThinkingLevel` | `ThinkingLevel` | Thinking intensity |
| | `Tools` | `[]Tool` | Available tools |
| | `Messages` | `[]AgentMessage` | Conversation history |
| | `IsStreaming` | `bool` | Currently streaming |
| | `StreamMessage` | `*AgentMessage` | Current streaming message |
| | `PendingToolCalls` | `map[string]bool` | Pending tool call IDs |
| | `Error` | `string` | Error state |
| | `SessionID` | `string` | Session identifier |
| | `UserID` | `string` | User identifier |
| | `AgentID` | `string` | Agent identifier |

### AgentOptions

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AgentOptions` | `ID` | `string` | Custom agent ID |
| | `SystemPrompt` | `string` | System prompt |
| | `SessionID` | `string` | Session ID |
| | `UserID` | `string` | User ID |
| | `AgentID` | `string` | Agent ID |
| | `LLMClient` | `LLMClient` | LLM client |

## NewAgent

```mermaid
flowchart TB
    Start(["NewAgent(opts) → *Agent"]) --> CheckID{opts.ID empty}
    CheckID -->|yes| GenID["uuid.New().String() → id"]
    CheckID -->|no| SetID["id = opts.ID"]
    GenID --> CheckUserID
    SetID --> CheckUserID{opts.UserID empty}
    CheckUserID -->|yes| SetUserID["userID = 'unknown'"]
    CheckUserID -->|no| SetUserID1["userID = opts.UserID"]
    SetUserID --> CheckAgentID
    SetUserID1 --> CheckAgentID{opts.AgentID empty}
    CheckAgentID -->|yes| SetAgentID["agentID = id"]
    CheckAgentID -->|no| SetAgentID1["agentID = opts.AgentID"]
    SetAgentID --> NewCtx
    SetAgentID1 --> NewCtx["context.WithCancel(context.Background()) → ctx, cancel"]
    NewCtx --> NewState["NewAgentState(opts) → state"]
    NewState --> ReturnAgent(["return &Agent{id, userID, agentID, state, ctx, cancel, opts.LLMClient}"])

    click NewState href "types.md"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,ReturnAgent mainFn
    class NewState localPkgs
```

## Subscribe

```mermaid
flowchart TB
    Start(["Subscribe() → (string, chan AgentEvent)"]) --> GenID["uuid.New().String() → subID"]
    GenID --> MakeCh["make(chan AgentEvent, 100) → ch"]
    MakeCh --> Lock["a.subscribersMu.Lock()"]
    Lock --> AddMap["a.subscribers[subID] = ch"]
    AddMap --> Unlock["a.subscribersMu.Unlock()"]
    Unlock --> Return(["return subID, ch"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## Unsubscribe

```mermaid
flowchart TB
    Start(["Unsubscribe(id string)"]) --> Lock["a.subscribersMu.Lock()"]
    Lock --> CheckExists{ch exists}
    CheckExists -->|yes| CloseCh["close(ch)"]
    CloseCh --> DeleteMap["delete(a.subscribers, id)"]
    DeleteExists --> Unlock["a.subscribersMu.Unlock()"]
    CheckExists -->|no| DeleteExists
    Unlock --> Return(["return"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## State

```mermaid
flowchart TB
    Start(["State() → AgentState"]) --> Lock["a.mu.RLock()"]
    Lock --> CopyMsgs["make([]AgentMessage, len(a.state.Messages)) → messages"]
    CopyMsgs --> CopyMsgData["copy(messages, a.state.Messages)"]
    CopyMsgData --> CopyPending["make(map[string]bool, len(a.state.PendingToolCalls)) → pendingToolCalls"]
    CopyPending --> CopyPendingLoop["for k, v in a.state.PendingToolCalls: pendingToolCalls[k] = v"]
    CopyPendingLoop --> CopyTools["make([]Tool, len(a.state.Tools)) → tools"]
    CopyTools --> CopyToolsData["copy(tools, a.state.Tools)"]
    CopyToolsData --> Unlock["a.mu.RUnlock()"]
    Unlock --> Return(["return AgentState{SystemPrompt, Model, ThinkingLevel, Tools, Messages, IsStreaming, StreamMessage, PendingToolCalls, Error, SessionID, UserID, AgentID}"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## Prompt

```mermaid
flowchart TB
    Start(["Prompt(ctx, message) → error"]) --> CheckCtx["ctx.Err() → err"]
    CheckCtx -->|error| Err1(["return err"])
    CheckCtx -->|ok| CheckLLM{a.llmClient nil}
    CheckLLM -->|yes| Err2(["return error: LLM client not configured"])
    CheckLLM -->|no| Lock["a.mu.Lock()"]
    Lock --> NewUserMsg["NewTextMessage(RoleUser, message) → userMsg"]
    NewUserMsg --> AddMsg["a.state.AddMessage(userMsg)"]
    AddMsg --> Unlock["a.mu.Unlock()"]
    Unlock --> EmitStart["a.emit(AgentStartEvent{Timestamp: userMsg.Timestamp})"]
    EmitStart --> SetStream["a.setStreamingState(true, nil)"]
    SetStream --> NewLoop["NewLoop(a.llmClient) → loop"]
    NewLoop --> GetMsgs["a.State() → finalState"]
    GetMsgs --> ExtractMsgs["finalState.Messages → messages"]
    ExtractMsgs --> RunLoop["loop.Run(ctx, messages, a.state.Tools...) → result, err"]
    RunLoop -->|error| ClearStreamErr["a.setStreamingState(false, nil)"]
    ClearStreamErr --> Err3(["return err"])
    RunLoop -->|ok| LockState["a.mu.Lock()"]
    LockState --> UpdateMsgs["a.state.Messages = result.Messages"]
    UpdateMsgs --> UnlockState["a.mu.Unlock()"]
    UnlockState --> GetFinalState["a.State() → finalState"]
    GetFinalState --> EmitEnd["a.emit(AgentEndEvent{Timestamp: finalState.Messages[len-1].Timestamp, Messages: finalState.Messages})"]
    EmitEnd --> ClearStream["a.setStreamingState(false, nil)"]
    ClearStream --> Return(["return nil"])

    click NewTextMessage href "types.md"
    click NewLoop href "loop.md"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class NewTextMessage,NewLoop localPkgs
    class Err1,Err2,Err3 errorFlow
```

## AddTools

```mermaid
flowchart TB
    Start(["AddTools(tools ...Tool)"]) --> Lock["a.mu.Lock()"]
    Lock --> Append["a.state.Tools = append(a.state.Tools, tools...)"]
    Append --> Unlock["a.mu.Unlock()"]
    Unlock --> Return(["return"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## RemoveTools

```mermaid
flowchart TB
    Start(["RemoveTools(prefix string)"]) --> Lock["a.mu.Lock()"]
    Lock --> MakeUpdated["make([]Tool, 0) → updated"]
    MakeUpdated --> ForTools["for tool in a.state.Tools"]
    ForTools --> CheckPrefix{"tool.Name starts with prefix"}
    CheckPrefix -->|no| AppendUpdated["updated = append(updated, tool)"]
    CheckPrefix -->|yes| LoopTools
    AppendUpdated --> LoopTools
    LoopTools --> ForTools
    ForTools -->|done| SetUpdated["a.state.Tools = updated"]
    SetUpdated --> Unlock["a.mu.Unlock()"]
    Unlock --> Return(["return"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## Design Notes

### Thread-Safe State Access

**Q: Why return a copy of state from State()?**

| Aspect | Description |
|--------|-------------|
| Context | Concurrent access to agent state |
| Options considered | Return pointer, return copy, lock externally |
| Rationale | Prevents external mutation of internal state |

**Current approach is recommended because:**
- Callers can't accidentally mutate state
- No need for external locking
- Safe to pass state to goroutines
- Standard immutable data pattern

### Event Channel Buffer

**Q: Why buffer events at 100?**

| Aspect | Description |
|--------|-------------|
| Context | Event channel capacity |
| Options considered | 10, 100, 1000, unbuffered |
| Rationale | 100 handles burst events without blocking |

**Current approach is recommended because:**
- Handles message streaming bursts
- Won't block sender on slow receiver
- Memory-efficient for most cases
- Subscriber can still fall behind gracefully

### Skip Full Channels

**Q: Why skip subscribers with full channels instead of blocking?**

| Aspect | Description |
|--------|-------------|
| Context | Event broadcasting |
| Options considered | Block, skip, drop oldest |
| Rationale | Prevent slow subscribers from blocking agent |

**Current approach is recommended because:**
- Agent never blocks on broadcast
- Fast subscribers get all events
- Slow subscribers gracefully degrade
- Non-blocking pattern for event systems

## Test Coverage

### Unit Tests (`agent_test.go`)

| Test Suite | Coverage |
|------------|----------|
| `TestNewAgent` | Agent creation |
| `TestNewAgent_WithID` | Custom ID |
| `TestSubscribe` | Event subscription |
| `TestUnsubscribe` | Event unsubscription |
| `TestState` | State retrieval |
| `TestPrompt_NoLLMClient` | Error without LLM client |

**Running:**
```bash
make test ./internal/agent
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/google/uuid` | UUID generation |

**File Locations:**
- [`internal/agent/agent.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/agent/agent.go) - Main agent implementation

## Subpackages

| Package | Description |
|---------|-------------|
| [loop.md](loop.md) | Tool calling loop |
| [tool.md](tool.md) | Tool execution |
| [events.md](events.md) | Event system |
| [types.md](types.md) | Core types |
| [tools.md](tools.md) | Built-in tools |

## Related

- [WebSocket Handler](../websocket.md) - Multi-agent routing via agent_id
- [Storage](../storage/README.md) - Workspace persistence
- [Mem0](../mem0/README.md) - Long-term memory tools
