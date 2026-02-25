# Session

Session management with timeout support and activity tracking.

## Overview

```mermaid
flowchart TB
    Start([GetOrCreateSession]) --> Lock["m.mu.Lock()"]
    Lock --> CheckExisting{matching session?}
    CheckExisting -->|yes| UpdateActivity["s.UpdateActivity()"]
    UpdateActivity --> Unlock["m.mu.Unlock()"]
    Unlock --> ReturnFound(["return s"])
    CheckExisting -->|no| NewSession["NewSession(ctx, userID, agentID) → newSession"]
    NewSession --> Store["m.sessions[newSession.SessionID()] = newSession"]
    Store --> Unlock2["m.mu.Unlock()"]
    Unlock2 --> ReturnNew(["return newSession"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,ReturnFound,ReturnNew mainFn
```

## Structs

### Manager

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Manager` | `mu` | `sync.RWMutex` | Thread-safe lock |
| | `sessions` | `map[string]*Session` | Session storage |
| | `logger` | `zerolog.Logger` | Logger |
| | `timeout` | `time.Duration` | Session timeout |

### Session

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Session` | `sessionID` | `string` | Unique session ID |
| | `userID` | `string` | User identifier |
| | `agentID` | `string` | Agent identifier |
| | `mem0SessionID` | `string` | Mem0 session ID |
| | `messages` | `[]Message` | Message history |
| | `startedAt` | `time.Time` | Session start time |
| | `lastActivity` | `time.Time` | Last activity time |

### Message

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Message` | `Role` | `string` | Message role |
| | `Content` | `string` | Message content |
| | `Timestamp` | `int64` | Unix timestamp |

## Functions

### NewManager

```mermaid
flowchart TB
    Start(["NewManager(logger, timeout)"]) --> CreateManager["&Manager{sessions, logger, timeout} → m"]
    CreateManager --> Done(["return m"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Done mainFn
```

### GetOrCreateSession

```mermaid
flowchart TB
    Start(["GetOrCreateSession"]) --> Lock["m.mu.Lock()"]
    Lock --> LoopSessions["for sessions"]

    subgraph SGLoopSessions1 [" "]
        LoopSessions --> GetSession["s := range"]
        GetSession --> CheckMatch{match?}
        CheckMatch -->|no| NextSession["continue"]
        CheckMatch -->|yes| UpdateActivity["s.UpdateActivity()"]
        UpdateActivity --> Unlock["m.mu.Unlock()"]
        Unlock --> ReturnFound(["return s"])
        NextSession --> LoopSessions
    end

    LoopSessions -->|done| NewSessionCall["NewSession(ctx) → newSession"]
    NewSessionCall --> StoreSession["m.sessions[id] = newSession"]
    StoreSession --> Unlock2["m.mu.Unlock()"]
    Unlock2 --> ReturnNew(["return newSession"])

    click NewSessionCall href "#NewSession"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,ReturnFound,ReturnNew mainFn
    class NewSessionCall,UpdateActivity localPkgs
    class LoopSessions,GetSession internalFn
```

### RemoveSession

```mermaid
flowchart TB
    Start(["RemoveSession(sessionID)"]) --> Lock["m.mu.Lock()"]
    Lock --> GetSession["m.sessions[sessionID] → s"]
    GetSession --> CheckExists{exists?}
    CheckExists -->|no| Unlock["m.mu.Unlock()"]
    Unlock --> Return(["return"])
    CheckExists -->|yes| DeleteSession["delete(m.sessions, sessionID)"]
    DeleteSession --> LogInfo["logger.Info()"]
    LogInfo --> Unlock2["m.mu.Unlock()"]
    Unlock2 --> ReturnDone(["return"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return,ReturnDone mainFn
```

### CleanupExpired

```mermaid
flowchart TB
    Start(["CleanupExpired"]) --> Lock["m.mu.Lock()"]
    Lock --> LoopSessions["for sessions"]

    subgraph SGLoopSessions2 [" "]
        LoopSessions --> GetSession["id, s := range"]
        GetSession --> CheckExpired{expired?}
        CheckExpired -->|yes| DeleteSession["delete(m.sessions, id)"]
        DeleteSession --> IncCount["count++"]
        CheckExpired -->|no| NextSession["continue"]
        IncCount --> NextSession
        NextSession --> LoopSessions
    end

    LoopSessions -->|done| CheckCount{count > 0?}
    CheckCount -->|yes| LogDebug["logger.Debug()"]
    CheckCount -->|no| Unlock
    LogDebug --> Unlock["m.mu.Unlock()"]
    Unlock --> Return(["return count"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class LoopSessions,GetSession internalFn
```

### StartCleanupLoop

```mermaid
flowchart TB
    Start(["StartCleanupLoop(interval)"]) --> NewTicker["time.NewTicker(interval) → ticker"]
    NewTicker --> GoLoop["go func()"]

    subgraph SGGoLoop [" "]
        GoLoop --> Loop["for range ticker.C"]
        Loop --> CleanupExpired["m.CleanupExpired() → count"]
        CleanupExpired --> Loop
    end

    click CleanupExpired href "#CleanupExpired"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start mainFn
    class CleanupExpired localPkgs
```

### NewSession

```mermaid
flowchart TB
    Start(["NewSession(ctx, userID, agentID)"]) --> NewNow["time.Now() → now"]
    NewNow --> NewUUID1["uuid.New().String() → sessionID"]
    NewUUID1 --> NewUUID2["uuid.New().String() → mem0SessionID"]
    NewUUID2 --> MakeMessages["make([]Message, 0) → messages"]

    subgraph SGCreateSession [" "]
        MakeMessages --> CreateSession["&Session{sessionID, userID, agentID, mem0SessionID, messages, now, now} → s"]
        CreateSession --> Done(["return s"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Done mainFn
    class CreateSession internalFn
```

### SessionID

```mermaid
flowchart TB
    Start(["SessionID()"]) --> Return(["return s.sessionID"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### UserID

```mermaid
flowchart TB
    Start(["UserID()"]) --> Return(["return s.userID"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### AgentID

```mermaid
flowchart TB
    Start(["AgentID()"]) --> Return(["return s.agentID"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### Mem0SessionID

```mermaid
flowchart TB
    Start(["Mem0SessionID()"]) --> Return(["return s.mem0SessionID"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### Messages

```mermaid
flowchart TB
    Start(["Messages()"]) --> MakeResult["make([]Message, len(s.messages)) → result"]
    MakeResult --> Copy["copy(result, s.messages)"]
    Copy --> Return(["return result"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### AddMessage

```mermaid
flowchart TB
    Start(["AddMessage(msg)"]) --> Append["s.messages = append(s.messages, msg)"]
    Append --> UpdateActivity["s.UpdateActivity()"]

    click UpdateActivity href "#UpdateActivity"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start mainFn
    class UpdateActivity localPkgs
```

### UpdateActivity

```mermaid
flowchart TB
    Start(["UpdateActivity()"]) --> SetTime["s.lastActivity = time.Now()"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start mainFn
```

### IsExpired

```mermaid
flowchart TB
    Start(["IsExpired(timeout)"]) --> CalcSince["time.Since(s.lastActivity) → duration"]
    CalcSince --> CheckSince{duration > timeout?}
    CheckSince -->|yes| ReturnTrue(["return true"])
    CheckSince -->|no| ReturnFalse(["return false"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,ReturnTrue,ReturnFalse mainFn
```

### MessageCount

```mermaid
flowchart TB
    Start(["MessageCount()"]) --> Return(["return len(s.messages)"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### ClearMessages

```mermaid
flowchart TB
    Start(["ClearMessages()"]) --> MakeEmpty["s.messages = make([]Message, 0)"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start mainFn
```

## Design Notes

### In-Memory Sessions

**Q: Why store sessions in memory instead of Redis?**

| Aspect | Description |
|--------|-------------|
| Context | Session persistence |
| Options considered | In-memory, Redis, database |
| Rationale | Sessions are ephemeral, agents hold real state |

**Current approach is recommended because:**
- Fast access for active sessions
- Agents hold persistent state separately
- Session timeout handles cleanup
- Can add Redis backup later

### Separate Mem0 Session ID

**Q: Why have a separate mem0SessionID?**

| Aspect | Description |
|--------|-------------|
| Context | Memory service integration |
| Options considered | Reuse sessionID, separate ID |
| Rationale | Different namespaces, independent lifecycle |

**Current approach is recommended because:**
- Mem0 has its own session concept
- Gateway session may outlive Mem0 session
- Clear separation of concerns
- Easier debugging

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestNewManager` | Manager creation |
| `TestGetOrCreateSession` | Session creation |
| `TestGetOrCreateSession_Existing` | Session reuse |
| `TestRemoveSession` | Session removal |
| `TestCleanupExpired` | Expiration cleanup |
| `TestSession_IsExpired` | Expiration check |
| `TestSession_AddMessage` | Message handling |

**Running:**
```bash
make test ./internal/session
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/google/uuid` | UUID generation |
| `github.com/rs/zerolog` | Logging |

**File Locations:**
- [`internal/session/manager.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/session/manager.go) - Session manager
- [`internal/session/session.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/session/session.go) - Session type

## Related

- [Agent](../agent/README.md) - Agent runtime
- [WebSocket](../websocket.md) - Connection handling
- [Mem0](../mem0/README.md) - Memory service
