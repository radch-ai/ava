# Agent

ADK-based agent runtime using Eino ADK's ChatModelAgent and Runner for LLM integration with tool execution.

## Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `defaultIdleTimeout` | `time.Duration` | `5 * time.Minute` | Default idle timeout before agent exits |
| `defaultLaneQueueSize` | `int` | `100` | Default task queue buffer size |

## Structs

### ADKAgent

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ADKAgent` | `agentID` | `string` | Unique agent instance ID |
| | `userID` | `string` | User identifier (multi-agent isolation) |
| | `ctx` | `context.Context` | Agent context |
| | `cancel` | `context.CancelFunc` | Context cancel function |
| | `laneCh` | `chan Task` | Task queue channel |
| | `mu` | `sync.Mutex` | State mutex |
| | `connRef` | `*WSConnRef` | Swappable WebSocket connection |
| | `einoAgent` | `*adk.ChatModelAgent` | Eino ADK agent |
| | `einoRunner` | `*adk.Runner` | Eino ADK runner |
| | `idleTimeout` | `time.Duration` | Idle timeout before exit |

### ADKAgentOptions

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ADKAgentOptions` | `AgentID` | `string` | Agent identifier |
| | `UserID` | `string` | User ID |
| | `Description` | `string` | Agent description |
| | `SystemPrompt` | `string` | System prompt for LLM |
| | `QuotaChatModel` | `QuotaChatModelInterface` | LLM client with quota |
| | `Memories` | `[]string` | User memories for context |
| | `IdleTimeout` | `time.Duration` | Idle timeout |

### Task

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Task` | `Message` | `string` | Task message content |
| | `MsgID` | `string` | Message identifier |

## Methods

Simple getters, setters, and lifecycle methods.

| Method | Signature | Description |
|--------|-----------|-------------|
| `Stop` | `()` | Cancels agent context |
| `GetContext` | `() context.Context` | Returns agent context |
| `GetAgentID` | `() string` | Returns agent ID |
| `GetUserID` | `() string` | Returns user ID |
| `Submit` | `(task Task)` | Submits task to lane queue |
| `GetConnRef` | `() *WSConnRef` | Returns WebSocket connection reference |
| `SetProcessTasksFn` | `(fn func([]Task))` | Sets custom task processor |
| `SetOnExit` | `(fn func())` | Sets exit callback |
| `GetStatus` | `() map[string]interface{}` | Returns agent status map with agent_id, user_id, queue_len, active |

## NewADKAgent

```mermaid
flowchart TB
    Start(["NewADKAgent(ctx, firstTask, opts) → *ADKAgent"]) --> CreateCtx["context.WithCancel(ctx) → ctx, cancel"]
    CreateCtx --> InitAgent["&ADKAgent{agentID, userID, ctx, cancel, laneCh, connRef} → a"]
    InitAgent --> CheckTimeout{opts.IdleTimeout?}
    CheckTimeout -->|zero| SetDefault["a.idleTimeout = defaultIdleTimeout (5m)"]
    CheckTimeout -->|set| UseProvided["a.idleTimeout = opts.IdleTimeout"]
    SetDefault --> QueueTask["a.laneCh ← firstTask"]
    UseProvided --> QueueTask
    QueueTask --> Return(["return a"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## Start

```mermaid
flowchart TB
    Start(["Start(opts) → error"]) --> GoLoop["go a.loop()"]
    GoLoop --> Return(["return nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## StartWithModel

```mermaid
flowchart TB
    Start(["StartWithModel(opts) → error"]) --> CheckModel{opts.QuotaChatModel nil?}
    CheckModel -->|yes| GoLoopNoModel["go a.loop()"]
    CheckModel -->|no| GetInner["opts.QuotaChatModel.GetInnerModel() → chatModel"]
    GetInner --> GetTools["GetGlobalTools() → tools"]
    GetTools --> BuildInstr["buildInstruction(opts.SystemPrompt, opts.Memories) → instruction"]
    BuildInstr --> NewAgent["adk.NewChatModelAgent(ctx, config) → einoAgent, err"]
    NewAgent -->|error| ReturnErr(["return err"])
    NewAgent -->|ok| NewRunner["adk.NewRunner(ctx, config) → einoRunner"]
    NewRunner --> GoLoop["go a.loop()"]
    GoLoop --> ReturnOk(["return nil"])
    GoLoopNoModel --> ReturnOk

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnOk mainFn
    class ReturnErr errorFlow
```

## GetGlobalTools

```mermaid
flowchart TB
    Start(["GetGlobalTools() → []tool.BaseTool"]) --> CreateCtx["context.Background() → ctx"]
    CreateCtx --> NewDdg["duckduckgo.NewTool(ctx, config) → ddgTool, err"]
    NewDdg -->|error| ReturnEmpty(["return []tool.BaseTool{}"])
    NewDdg -->|ok| ReturnTools(["return []tool.BaseTool{ddgTool}"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnEmpty,ReturnTools mainFn
```

## loop

Private method that runs the agent's main event loop.

```mermaid
flowchart TB
    Start(["loop()"]) --> NewTimer["time.NewTimer(idleTimeout) → idleTimer"]
    NewTimer --> LoopStart{{"loop"}}

    subgraph SGLoop [" "]
        LoopStart --> Select["select"]
        Select -->|laneCh| ReceiveTask["receive task from laneCh"]
        ReceiveTask --> ResetTimer["idleTimer.Reset(idleTimeout)"]
        ResetTimer --> DrainMsgs["a.drainMessages(task) → tasks"]
        DrainMsgs --> CheckFn{{"processTasksFn?"}}
        CheckFn -->|yes| CallFn["a.processTasksFn(tasks)"]
        CheckFn -->|no| ProcessTasks["a.processTasks(tasks)"]
        CallFn --> LoopStart
        ProcessTasks --> LoopStart

        Select -->|idleTimer.C| CheckQueue{{"laneCh empty?"}}
        CheckQueue -->|yes| CallExit["a.onExit()"]
        CallExit --> Return(["return"])
        CheckQueue -->|no| ResetTimer2["idleTimer.Reset(idleTimeout)"]
        ResetTimer2 --> LoopStart

        Select -->|ctx.Done| CallExit2["a.onExit()"]
        CallExit2 --> Return2(["return"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return,Return2 mainFn
```

## processTasks

Private method that processes accumulated tasks.

```mermaid
flowchart TB
    Start(["processTasks(tasks)"]) --> CheckLen{{"len(tasks) == 0?"}}
    CheckLen -->|yes| Return(["return"])
    CheckLen -->|no| BuildInput["strings.Builder → input"]
    BuildInput --> ForTasks["iterate tasks"]
    ForTasks --> AppendMsg["input.WriteString(t.Message)"]
    AppendMsg --> CheckMsgID{{"msgID empty and t.MsgID set?"}}
    CheckMsgID -->|yes| SetMsgID["msgID = t.MsgID"]
    CheckMsgID -->|no| LoopNext
    SetMsgID --> LoopNext["continue loop"]
    LoopNext --> ForTasks
    ForTasks -->|done| CheckRunner{{"einoRunner?"}}
    CheckRunner -->|yes| Query["einoRunner.Query(ctx, input.String())"]
    CheckRunner -->|no| CheckConn
    Query --> CheckConn{{"connRef.IsSet()?"}}
    CheckConn -->|no| NotifyUser["a.notifyUser(msgID, input.String())"]
    CheckConn -->|yes| Return2(["return"])
    NotifyUser --> Return2

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return,Return2 mainFn
```

## WSConnRef

Swappable WebSocket connection reference for thread-safe connection management.

### WSConnRef Methods

| Method | Description |
|--------|-------------|
| `Get()` | Returns current connection (thread-safe) |
| `Set(conn)` | Sets new connection (thread-safe) |
| `Clear()` | Clears connection reference |
| `IsSet()` | Returns true if connection is set |

### NewWSConnRef

```mermaid
flowchart TB
    Start(["NewWSConnRef() → *WSConnRef"]) --> Return(["return &WSConnRef{}"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## Design Notes

### Eino ADK Integration

**Q: Why use Eino ADK's ChatModelAgent instead of custom implementation?**

| Aspect | Description |
|--------|-------------|
| Context | LLM agent implementation |
| Options considered | Custom implementation, Eino ADK, other frameworks |
| Rationale | Eino ADK provides battle-tested agent patterns with tool calling |

**Current approach is recommended because:**
- Standard tool calling patterns
- Built-in message coalescing
- Runner handles orchestration
- Easier to maintain and upgrade

### Message Coalescing with drainMessages

**Q: Why drain messages instead of processing one at a time?**

| Aspect | Description |
|--------|-------------|
| Context | High-frequency message handling |
| Options considered | Process each, batch process, drain |
| Rationale | Reduces LLM API calls for rapid user input |

**Current approach is recommended because:**
- Combines multiple rapid messages
- Reduces API costs
- Better user experience
- Handles mobile network fluctuations

### Idle Timeout Pattern

**Q: Why use idle timeout instead of explicit cleanup?**

| Aspect | Description |
|--------|-------------|
| Context | Agent lifecycle management |
| Options considered | Explicit cleanup, idle timeout, never cleanup |
| Rationale | Balances resource usage with conversation continuity |

**Current approach is recommended because:**
- Agents auto-cleanup after inactivity
- User can reconnect and continue
- Prevents resource leaks
- Configurable timeout

### WSConnRef for Connection Swapping

**Q: Why use WSConnRef instead of direct connection?**

| Aspect | Description |
|--------|-------------|
| Context | WebSocket reconnection handling |
| Options considered | Direct connection, connection reference, channel-based |
| Rationale | Allows connection swapping without agent restart |

**Current approach is recommended because:**
- Agent survives connection drops
- User can reconnect seamlessly
- Thread-safe connection access
- Clean separation of concerns

## Test Coverage

### Unit Tests (`adk_agent_test.go`, `conn_ref_test.go`)

| Test Suite | Coverage |
|------------|----------|
| `TestNewADKAgent_CreatesLaneCh` | Agent creation with channel |
| `TestNewADKAgent_QueuesFirstMessage` | First task queued |
| `TestADKAgent_Stop` | Context cancellation |
| `TestADKAgent_Loop_DrainsMessages` | Message draining |
| `TestADKAgent_Loop_IdleExit` | Idle timeout exit |
| `TestADKAgent_Loop_ContextCancel` | Context cancel exit |
| `TestBuildInstruction` | Instruction building with memories |
| `TestGetGlobalTools_ReturnsTools` | Tool retrieval |
| `TestWSConnRef_GetSetClear` | Connection reference operations |
| `TestWSConnRef_Concurrent` | Thread safety |

**Running:**
```bash
make test ./internal/agent
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/cloudwego/eino/adk` | Eino ADK agent framework |
| `github.com/cloudwego/eino/components/model` | LLM model interface |
| `github.com/cloudwego/eino/components/tool` | Tool interface |
| `github.com/cloudwego/eino-ext/components/tool/duckduckgo` | DuckDuckGo search tool |
| `github.com/gorilla/websocket` | WebSocket connection |
| `sync` | Thread safety |

**File Locations:**
- [`internal/agent/adk_agent.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/agent/adk_agent.go) - ADKAgent implementation
- [`internal/agent/conn_ref.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/agent/conn_ref.go) - WSConnRef
- [`internal/agent/types.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/agent/types.go) - Core types

## Subpackages

| Package | Description |
|---------|-------------|
| [types.md](types.md) | Core types (AgentMessage, Tool, etc.) |
| [tools.md](tools.md) | Built-in tools |

## Related

- [WebSocket Handler](../websocket.md) - Agent lifecycle management
- [Storage](../storage/README.md) - Workspace persistence
- [Mem0](../mem0/README.md) - Long-term memory tools
