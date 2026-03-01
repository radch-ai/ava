# WebSocket

WebSocket connection handler with multi-agent routing and connection management.

## Overview

```mermaid
flowchart TB
    Start([WebSocket Request]) --> CheckSem{connSem?}
    CheckSem -->|yes| Acquire["select h.connSem"]
    CheckSem -->|no| UpgradeConn
    Acquire -->|full| LimitErr(["write 503"])
    Acquire -->|slot| UpgradeConn["h.upgrader.Upgrade(w, r, nil) → conn, err"]
    UpgradeConn -->|error| UpgradeErr(["return"])
    UpgradeConn -->|ok| IncActive["atomic.AddInt32(&activeConns, 1)"]
    IncActive --> GetUserID["ctx.GetUserIDFromContext(r.Context()) → userID"]
    GetUserID --> CheckUser{userID?}
    CheckUser -->|yes| SetSession["sessionID = userID"]
    CheckUser -->|no| NewUUID["uuid.New().String() → sessionID"]
    SetSession --> NewUUID
    NewUUID --> CreateCtx["context.WithCancel() → ctx, cancel"]
    CreateCtx --> MakeWriteCh["make(chan *wsMessage, 8) → writeCh"]
    MakeWriteCh --> SendEvent["marshalEvent(connectedEvent) → msg"]
    SendEvent --> WriteCh["writeCh ← msg"]
    WriteCh --> MsgLoop{message loop}
    MsgLoop -->|text| HandleText["h.handleTextMessage(ctx, message, userID, writeCh)"]
    MsgLoop -->|close| Cancel(["cancel()"])
    HandleText --> Unmarshal["json.Unmarshal(data, &msg)"]
    Unmarshal --> CheckType{msg.Type?}
    CheckType -->|prompt| ValidatePrompt{agentID? text?}
    ValidatePrompt -->|missing| Skip(["return"])
    ValidatePrompt -->|valid| GetAgent["h.agentManager.GetAgent(ctx, userID, msg.AgentID, connWrapper, task) → agentInstance"]
    GetAgent --> SubmitTask["agentInstance.Submit(task)"]
    SubmitTask --> MsgLoop

    click GetUserID href "ctx.md"
    click GetAgent href "#GetAgent"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Cancel,Skip mainFn
    class GetUserID,GetAgent localPkgs
    class LimitErr,UpgradeErr errorFlow
```

## Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `defaultBufferSize` | `int` | `8192` | 8KB read/write buffer |
| `MaxMessageSize` | `int` | `65536` | 64KB max message |
| `readTimeout` | `Duration` | `60s` | Read deadline |
| `writeTimeout` | `Duration` | `10s` | Write deadline |
| `pingInterval` | `Duration` | `30s` | Ping frequency |

## Structs

### Handler

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Handler` | `logger` | `zerolog.Logger` | Logger |
| | `upgrader` | `websocket.Upgrader` | WebSocket upgrader |
| | `connSem` | `chan struct{}` | Connection semaphore |
| | `maxConns` | `int` | Max connections (0=unlimited) |
| | `activeConns` | `int32` | Active connection count (atomic) |
| | `agentManager` | `*AgentManager` | Agent manager |

### wsMessage

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `wsMessage` | `messageType` | `int` | WebSocket message type |
| | `data` | `[]byte` | Message data |

### wsEvent

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `wsEvent` | `Type` | `string` | Event type |
| | `AgentID` | `string` | Agent identifier |
| | `Data` | `any` | Event payload |

### AgentKey

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AgentKey` | `UserID` | `string` | User identifier |
| | `AgentID` | `string` | Agent identifier |

### AgentManager

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AgentManager` | `mu` | `sync.Mutex` | Thread-safe lock |
| | `agents` | `map[AgentKey]*agent.ADKAgent` | Agent instances |
| | `logger` | `zerolog.Logger` | Logger |
| | `quotaChatModel` | `agent.QuotaChatModelInterface` | LLM client with quota |

### AgentManagerOptions

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AgentManagerOptions` | `QuotaChatModel` | `agent.QuotaChatModelInterface` | LLM client with quota |

## Functions

### NewHandler

```mermaid
flowchart TB
    Start(["NewHandler(logger, allowedOrigins, maxConns, agentOpts)"]) --> CopyUpgrader["handlerUpgrader = upgrader"]
    CopyUpgrader --> SetOrigin["handlerUpgrader.CheckOrigin = checkOrigin"]

    subgraph SGCheckOrigin [" "]
        SetOrigin --> CheckOriginDef{maxConns?}
        CheckOriginDef -->|>0| MakeSem["make(chan struct{}, maxConns) → sem"]
        CheckOriginDef -->|0| NoSem["sem = nil"]
    end

    MakeSem --> CreateHandler
    NoSem --> CreateHandler

    subgraph SGCreateHandler [" "]
        CreateHandler["&Handler{logger, handlerUpgrader, sem, maxConns, agentManager} → h"]
        CreateHandler --> Done(["return h"])
    end

    click SetOrigin href "#checkOrigin"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Done mainFn
    class MakeSem,CreateHandler internalFn
```

### checkOrigin

```mermaid
flowchart TB
    Start(["checkOrigin(r, allowedOrigins)"]) --> GetOrigin["r.Header.Get(Origin) → origin"]
    GetOrigin --> CheckOriginEmpty{origin?}
    CheckOriginEmpty -->|empty| ReturnTrue(["return true"])
    CheckOriginEmpty -->|set| LoopOrigins["for allowedOrigins"]

    subgraph SGLoopOrigins [" "]
        LoopOrigins --> CheckMatch{allowed==origin?}
        CheckMatch -->|yes| ReturnTrue2(["return true"])
        CheckMatch -->|no| NextAllowed["continue"]
        CheckMatch -->|wildcard| ReturnTrue2
        CheckMatch -->|other| NextAllowed
        NextAllowed --> LoopOrigins
    end

    LoopOrigins -->|done| ReturnFalse(["return false"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,ReturnTrue,ReturnTrue2,ReturnFalse mainFn
```

### HandleWebSocket

```mermaid
flowchart TB
    Start(["HandleWebSocket(w, r)"]) --> CheckSem{connSem?}
    CheckSem -->|yes| SelectSem["select h.connSem"]
    SelectSem -->|full| Write503(["http.Error(w, 503)"])
    SelectSem -->|slot| IncActive
    CheckSem -->|no| IncActive["atomic.AddInt32(&activeConns, 1)"]

    subgraph SGUpgrade [" "]
        IncActive --> Upgrade["h.upgrader.Upgrade(w, r, nil) → conn, err"]
        Upgrade --> CheckErr{err?}
        CheckErr -->|yes| DecActive(["atomic.AddInt32(&activeConns, -1)"])
        CheckErr -->|no| SetReadLimit["conn.SetReadLimit(maxMessageSize)"]
    end

    SetReadLimit --> NewUUID["uuid.New().String() → connID"]
    NewUUID --> GetUserID["ctx.GetUserIDFromContext(r.Context()) → userID"]
    GetUserID --> CheckUserID{userID?}
    CheckUserID -->|yes| SetSessionID["sessionID = userID"]
    CheckUserID -->|no| NewSessionID["uuid.New().String() → sessionID"]
    SetSessionID --> MakeContext["context.WithCancel() → ctx, cancel"]
    NewSessionID --> MakeContext

    subgraph SGMakeWriteCh [" "]
        MakeContext --> MakeWriteCh["make(chan *wsMessage, 8) → writeCh"]
        MakeWriteCh --> MakeWriterErrCh["make(chan error, 1) → writerErrCh"]
        MakeWriterErrCh --> GoWriter["go h.writer(ctx, conn, writeCh, writerErrCh)"]
    end

    GoWriter --> GoPing["go h.pingLoop(ctx, writeCh)"]
    GoPing --> MarshalEvent["marshalEvent(connectedEvent) → msg"]
    MarshalEvent --> SendWriteCh["writeCh ← msg"]
    SendWriteCh --> MessageLoop["for message loop"]

    subgraph SGMessageLoop [" "]
        MessageLoop --> SelectLoop["select"]
        SelectLoop -->|ctx.Done| LogClose["logger.Info()"]
        LogClose --> SendCloseFrame
        SelectLoop -->|writerErrCh| LogWriteErr["logger.Error()"]
        LogWriteErr --> Cancel(["cancel()"])
        SelectLoop -->|default| SetReadDeadline["conn.SetReadDeadline(readTimeout)"]
        SetReadDeadline --> ReadMessage["conn.ReadMessage() → messageType, message, err"]
        ReadMessage --> CheckReadErr{err?}
        CheckReadErr -->|yes| Cancel
        CheckReadErr -->|no| CheckText{messageType?}
        CheckText -->|TextMessage| HandleText["h.handleTextMessage(ctx, message, userID, writeCh)"]
        CheckText -->|other| EchoMessage["writeCh ← message"]
        HandleText --> MessageLoop
        EchoMessage --> MessageLoop
    end

    subgraph SGSendCloseFrame [" "]
        LogClose --> SendCloseFrame["select closeFrame"]
        SendCloseFrame -->|timeout| ReturnClose(["return"])
        SendCloseFrame -->|sent| StopTimer["timer.Stop()"]
        StopTimer --> ReturnClose
    end

    click GetUserID href "../ctx.md"
    click HandleText href "#handleTextMessage"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Write503,Cancel,ReturnClose mainFn
    class GetUserID,HandleText localPkgs
    class Upgrade,MakeWriteCh,GoWriter,GoPing,MarshalEvent,SendWriteCh,MessageLoop,SendCloseFrame internalFn
    class DecActive errorFlow
```

### writer

```mermaid
flowchart TB
    Start(["writer(ctx, conn, writeCh, logger, writerErrCh)"]) --> Loop["for"]
    Loop --> SelectLoop["select"]

    subgraph SGSelectLoop [" "]
        SelectLoop -->|ctx.Done| Return(["return"])
        SelectLoop -->|writeCh| ReceiveMsg["msg := <-writeCh"]
        ReceiveMsg --> CheckMsg{msg.messageType?}
        CheckMsg -->|0| PutPool["wsMsgPool.Put(msg)"]
        PutPool --> Loop
        CheckMsg -->|set| SetWriteDeadline["conn.SetWriteDeadline(writeTimeout)"]
    end

    SetWriteDeadline --> WriteMessage["conn.WriteMessage(msg.messageType, msg.data) → err"]
    WriteMessage --> CheckWriteErr{err?}
    CheckWriteErr -->|yes| ResetPutPool["msg.messageType = 0, msg.data = nil"]
    ResetPutPool --> PutPool2["wsMsgPool.Put(msg)"]
    PutPool2 --> SendWriterErr["select writerErrCh ← err"]
    SendWriterErr -->|sent| ReturnErr(["return"])
    SendWriterErr -->|ctx.Done| ReturnErr
    CheckWriteErr -->|no| ResetPutPool3["msg.messageType = 0, msg.data = nil"]
    ResetPutPool3 --> PutPool3["wsMsgPool.Put(msg)"]
    PutPool3 --> Loop

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return,ReturnErr mainFn
    class Loop,SelectLoop,ReceiveMsg,SetWriteDeadline,WriteMessage,SendWriterErr internalFn
```

### pingLoop

```mermaid
flowchart TB
    Start(["pingLoop(ctx, writeCh, logger)"]) --> NewTicker["time.NewTicker(pingInterval) → ticker"]
    NewTicker --> Loop["for"]

    subgraph SGLoop [" "]
        Loop --> SelectLoop["select"]
        SelectLoop -->|ctx.Done| StopTicker["ticker.Stop()"]
        StopTicker --> Return(["return"])
        SelectLoop -->|ticker.C| NewPingTimer["time.NewTimer(writeTimeout) → pingTimer"]
        NewPingTimer --> GetMsg["wsMsgPool.Get() → msg"]
        GetMsg --> SetPingMsg["msg.messageType = PingMessage, msg.data = nil"]
        SetPingMsg --> SelectSend["select send"]
    end

    subgraph SGSelectSend [" "]
        SelectSend -->|writeCh| StopTimer["pingTimer.Stop()"]
        StopTimer --> Loop
        SelectSend -->|ctx.Done| StopTimer2["pingTimer.Stop()"]
        StopTimer2 --> ResetPutPool["msg.messageType = 0, msg.data = nil"]
        ResetPutPool --> PutPool["wsMsgPool.Put(msg)"]
        PutPool --> ReturnPing(["return"])
        SelectSend -->|timer.C| StopTimer3["pingTimer.Stop()"]
        StopTimer3 --> ResetPutPool2["msg.messageType = 0, msg.data = nil"]
        ResetPutPool2 --> PutPool2["wsMsgPool.Put(msg)"]
        PutPool2 --> LogDebug["logger.Debug()"]
        LogDebug --> ReturnPing
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return,ReturnPing mainFn
    class Loop,SelectLoop,NewPingTimer,SetPingMsg,SelectSend internalFn
```

### handleTextMessage

```mermaid
flowchart TB
    Start(["handleTextMessage(ctx, data, userID, writeCh, logger)"]) --> Unmarshal["json.Unmarshal(data, &msg) → err"]
    Unmarshal --> CheckUnmarshalErr{err?}
    CheckUnmarshalErr -->|yes| Return(["return"])
    CheckUnmarshalErr -->|no| SwitchType{msg.Type?}

    subgraph SGPrompt [" "]
        SwitchType -->|prompt| CheckID{msg.ID?}
        CheckID -->|empty| LogDebug0["logger.Debug()"]
        LogDebug0 --> Return0(["return"])
        CheckID -->|set| CheckAgentID{msg.AgentID?}
        CheckAgentID -->|empty| LogDebug1["logger.Debug()"]
        LogDebug1 --> Return1(["return"])
        CheckAgentID -->|set| CheckText{msg.Text?}
        CheckText -->|empty| LogDebug2["logger.Debug()"]
        LogDebug2 --> Return1
        CheckText -->|set| CreateWrapper["NewWSConnWrapper(writeCh) → connWrapper"]
        CreateWrapper --> CreateTask["agent.Task{Message: msg.Text, MsgID: msg.ID} → task"]
        CreateTask --> GetAgent["h.agentManager.GetAgent(ctx, userID, msg.AgentID, connWrapper, task) → ag"]
    end

    GetAgent --> CheckAgent{ag nil?}
    CheckAgent -->|yes| LogDebug3["logger.Debug()"]
    LogDebug3 --> Return2(["return"])
    CheckAgent -->|no| CreateTask2["agent.Task{Message: msg.Text, MsgID: msg.ID} → task"]
    CreateTask2 --> SubmitTask["ag.Submit(task)"]
    SubmitTask --> Return2

    SwitchType -->|steer| CheckSteerID{msg.AgentID?}
    CheckSteerID -->|empty| LogDebug4["logger.Debug()"]
    LogDebug4 --> Return3(["return"])
    CheckSteerID -->|set| GetSteerAgent["h.agentManager.GetAgent(ctx, userID, msg.AgentID, nil, task) → ag"]
    GetSteerAgent --> CheckSteerAgent{ag nil?}
    CheckSteerAgent -->|yes| LogDebug5["logger.Debug()"]
    LogDebug5 --> Return3
    CheckSteerAgent -->|no| StopAgent["ag.Stop()"]
    StopAgent --> Return3

    SwitchType -->|status| CheckStatusID{msg.AgentID?}
    CheckStatusID -->|empty| LogDebug6["logger.Debug()"]
    LogDebug6 --> Return4(["return"])
    CheckStatusID -->|set| GetStatusAgent["h.agentManager.GetOrCreateAgentForUser(ctx, userID, msg.AgentID, task) → agentInstance"]
    GetStatusAgent --> GetStatus["agentInstance.GetStatus() → status"]
    GetStatus --> MarshalStatus["marshalEvent(wsEvent{Type: lane_status, AgentID, Data: status}) → msg"]
    MarshalStatus --> SendStatus["writeCh ← msg"]
    SendStatus --> Return4

    SwitchType -->|other| LogDebug7["logger.Debug()"]
    LogDebug7 --> Return5(["return"])

    click GetAgent href "#GetAgent"
    click GetOrCreateAgentForUser href "#GetOrCreateAgentForUser"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return,Return0,Return1,Return2,Return3,Return4,Return5 mainFn
    class GetAgent,GetOrCreateAgentForUser localPkgs
    class CreateWrapper,CreateTask,SubmitTask internalFn
```

### ActiveConns

```mermaid
flowchart TB
    Start(["ActiveConns()"]) --> LoadInt32["atomic.LoadInt32(&activeConns) → val"]
    LoadInt32 --> Return(["return int(val)"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### NewAgentManager

```mermaid
flowchart TB
    Start(["NewAgentManager(logger, opts)"]) --> CheckOpts{opts?}
    CheckOpts -->|yes| GetModel["opts.QuotaChatModel → quotaChatModel"]
    CheckOpts -->|no| NoModel["quotaChatModel = nil"]

    subgraph SGCreateManager [" "]
        GetModel --> CreateManager["&AgentManager{agents, logger, quotaChatModel} → m"]
        NoModel --> CreateManager
        CreateManager --> Done(["return m"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Done mainFn
    class CreateManager internalFn
```

### GetOrCreateAgentForUser

```mermaid
flowchart TB
    Start(["GetOrCreateAgentForUser(ctx, userID, agentID, firstTask)"]) --> MakeKey["AgentKey{userID, agentID} → key"]
    MakeKey --> Lock["m.mu.Lock()"]
    Lock --> GetExisting["m.agents[key] → existingAgent"]
    GetExisting --> CheckExists{exists?}
    CheckExists -->|yes| Unlock["m.mu.Unlock()"]
    Unlock --> ReturnExisting(["return existingAgent"])
    CheckExists -->|no| CreateAgent

    subgraph SGCreateAgent [" "]
        CreateAgent --> NewAgent["agent.NewADKAgent(ctx, firstTask, opts) → newAgent"]
        NewAgent --> StoreAgent["m.agents[key] = newAgent"]
        StoreAgent --> Unlock2["m.mu.Unlock()"]
    end

    Unlock2 --> LoadConfig["m.loadAgentConfig(userID, agentID) → config"]
    LoadConfig --> StartModel["newAgent.StartWithModel(opts)"]
    StartModel --> ReturnNew(["return newAgent"])

    click NewAgent href "../agent/README.md#NewADKAgent"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,ReturnExisting,ReturnNew mainFn
    class NewAgent localPkgs
    class CreateAgent,StoreAgent internalFn
```

### GetAgent

```mermaid
flowchart TB
    Start(["GetAgent(ctx, userID, agentID, conn, task)"]) --> MakeKey["AgentKey{userID, agentID} → key"]
    MakeKey --> Lock["m.mu.Lock()"]
    Lock --> GetExisting["m.agents[key] → existingAgent"]
    GetExisting --> CheckExists{exists?}
    CheckExists -->|yes| SubmitTask["existingAgent.Submit(task)"]
    SubmitTask --> Unlock["m.mu.Unlock()"]
    Unlock --> ReturnAdapter(["return &adkAgentAdapter{agent, conn, agentID}"])
    CheckExists -->|no| CreateAgent

    subgraph SGCreateAgent [" "]
        CreateAgent --> NewAgent["agent.NewADKAgent(ctx, task, opts) → newAgent"]
        NewAgent --> StoreAgent["m.agents[key] = newAgent"]
        StoreAgent --> Unlock2["m.mu.Unlock()"]
    end

    Unlock2 --> LoadConfig["m.loadAgentConfig(userID, agentID) → config"]
    LoadConfig --> StartModel["newAgent.StartWithModel(opts)"]
    StartModel --> ReturnAdapter2(["return &adkAgentAdapter{agent, conn, agentID}"])

    click NewAgent href "../agent/README.md#NewADKAgent"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,ReturnAdapter,ReturnAdapter2 mainFn
    class NewAgent localPkgs
    class CreateAgent,StoreAgent internalFn
```

### RemoveAgentForUser

```mermaid
flowchart TB
    Start(["RemoveAgentForUser(userID, agentID)"]) --> MakeKey["AgentKey{userID, agentID} → key"]
    MakeKey --> Lock["m.mu.Lock()"]
    Lock --> GetAgent["m.agents[key] → existingAgent"]
    GetAgent --> CheckExists{exists?}
    CheckExists -->|no| Unlock["m.mu.Unlock()"]
    Unlock --> Return(["return"])
    CheckExists -->|yes| StopAgent["existingAgent.Stop()"]
    StopAgent --> DeleteAgent["delete(m.agents, key)"]
    DeleteAgent --> Unlock2["m.mu.Unlock()"]
    Unlock2 --> ReturnDone(["return"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return,ReturnDone mainFn
```

## Design Notes

### Single Writer Pattern

**Q: Why use a single writer goroutine?**

| Aspect | Description |
|--------|-------------|
| Context | WebSocket concurrent writes |
| Options considered | Mutex, channel serialization, lock-free |
| Rationale | WebSocket doesn't support concurrent writes |

**Current approach is recommended because:**
- gorilla/websocket is not thread-safe for writes
- Channel serialization is cleaner than mutex
- Naturally handles ping/pong interleaving
- Writer goroutine can manage deadlines

### sync.Pool for Messages

**Q: Why use sync.Pool for wsMessage?**

| Aspect | Description |
|--------|-------------|
| Context | High-frequency message allocation |
| Options considered | New allocation, sync.Pool, buffer reuse |
| Rationale | Reduces GC pressure in high-traffic scenarios |

**Current approach is recommended because:**
- Messages are short-lived
- Pool reuses memory across connections
- Standard Go pattern for high-throughput
- Automatic GC of unused items

### Agents Persist Beyond Connection

**Q: Why do agents persist after WebSocket disconnect?**

| Aspect | Description |
|--------|-------------|
| Context | Agent lifecycle management |
| Options considered | Destroy on disconnect, persist, timed cleanup |
| Rationale | User can reconnect and continue conversation |

**Current approach is recommended because:**
- Better UX - conversation continues
- State (workspace, memory) preserved
- Supports mobile app backgrounding
- Agents have idle timeout for cleanup

### Composite Key for Multi-Agent

**Q: Why use (userID, agentID) composite key?**

| Aspect | Description |
|--------|-------------|
| Context | Agent isolation |
| Options considered | Single key, composite key, hierarchical |
| Rationale | Users can have multiple agents |

**Current approach is recommended because:**
- Clear ownership boundaries
- Prevents agent ID collisions between users
- Efficient map lookup
- Natural multi-tenancy

### ADKAgent Integration

**Q: Why use ADKAgent instead of custom Agent implementation?**

| Aspect | Description |
|--------|-------------|
| Context | Agent implementation |
| Options considered | Custom Agent, ADKAgent, other frameworks |
| Rationale | Eino ADK provides battle-tested patterns |

**Current approach is recommended because:**
- Standard tool calling patterns
- Built-in message handling
- Easier to maintain and upgrade
- Consistent with Eino ecosystem

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestNewHandler` | Handler creation |
| `TestHandleWebSocket` | Connection handling |
| `TestCheckOrigin` | Origin validation |
| `TestAgentManager` | Agent lifecycle |
| `TestGetOrCreateAgentForUser` | Agent creation |
| `TestGetAgent` | Agent retrieval |

**Running:**
```bash
make test ./internal/websocket
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/gorilla/websocket` | WebSocket protocol |
| `github.com/google/uuid` | UUID generation |
| `github.com/rs/zerolog` | Logging |
| `internal/agent` | Agent runtime |
| `internal/ctx` | Context utilities |

**File Locations:**
- [`internal/websocket/handler.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/websocket/handler.go) - WebSocket handler
- [`internal/websocket/agent_manager.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/websocket/agent_manager.go) - Agent manager

## Related

- [Agent](../agent/README.md) - Agent runtime
- [Server](../server/README.md) - HTTP server
