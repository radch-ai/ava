# Handler

WebSocket connection handler with origin validation, connection limiting, and message routing.

## Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `defaultBufferSize` | `int` | `8192` | 8KB read/write buffer per connection |
| `MaxMessageSize` | `int` | `65536` | 64KB max message size |
| `readTimeout` | `time.Duration` | `60s` | Read deadline timeout |
| `writeTimeout` | `time.Duration` | `10s` | Write deadline timeout |
| `pingInterval` | `time.Duration` | `30s` | Ping frame interval |

## Variables

| Variable | Type | Description |
|----------|------|-------------|
| `upgrader` | `websocket.Upgrader` | Package-level upgrader with 8KB buffers |
| `wsMsgPool` | `sync.Pool` | Pool for wsMessage structs |

## Structs

### Handler

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Handler` | `logger` | `zerolog.Logger` | Structured logger |
| | `upgrader` | `websocket.Upgrader` | HTTP to WebSocket upgrader |
| | `connSem` | `chan struct{}` | Semaphore for connection limiting |
| | `maxConns` | `int` | Maximum connections (0 = unlimited) |
| | `activeConns` | `int32` | Active connection count (atomic) |
| | `agentManager` | `*AgentManager` | Agent lifecycle manager |

### wsMessage

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `wsMessage` | `messageType` | `int` | WebSocket message type (text/binary/ping) |
| | `data` | `[]byte` | Message payload |

### wsEvent

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `wsEvent` | `Type` | `string` | Event type (connected, agent_start, etc.) |
| | `AgentID` | `string` | Agent identifier (optional) |
| | `Data` | `any` | Event payload |

## marshalEvent

Private function that converts an event struct to JSON bytes.

```mermaid
flowchart LR
    A(["marshalEvent"]) --> B["json.Marshal"]
    B --> C(["return data"])
```

## NewHandler

Creates a new WebSocket handler with connection limiting and origin validation.

```mermaid
flowchart TB
    A(["NewHandler"]) --> B["copy upgrader"]
    B --> C["set CheckOrigin"]
    C --> D{"maxConns > 0?"}
    D -->|yes| E["make semaphore"]
    D -->|no| F["connSem = nil"]
    E --> G["NewAgentManager"]
    F --> G
    G --> H(["return h"])
```

## checkOrigin

Private function that validates the Origin header against allowed origins list.

```mermaid
flowchart TB
    A(["checkOrigin"]) --> B["get Origin header"]
    B --> C{"origin empty?"}
    C -->|yes| D(["return true"])
    C -->|no| E["check allowed origins"]
    E --> F(["return result"])
```

## HandleWebSocket

Main WebSocket connection handler with message routing.

```mermaid
flowchart TB
    A(["HandleWebSocket"]) --> B{"connSem != nil?"}
    B -->|yes| C["try acquire"]
    B -->|no| D["increment active"]
    C --> E{"acquired?"}
    E -->|full| F(["503 error"])
    E -->|ok| D
    D --> G["upgrader.Upgrade"]
    G --> H{"ok?"}
    H -->|error| I(["return"])
    H -->|ok| J["setup context"]
    J --> K["go writer"]
    K --> L["go pingLoop"]
    L --> M["message loop"]
    M --> N(["return"])
```

## writer

Private method - single-writer goroutine for WebSocket writes with sync.Pool.

```mermaid
flowchart TB
    A(["writer"]) --> B["select"]
    B --> C{"msg type?"}
    C -->|ctx.Done| D(["return"])
    C -->|close| E["pool.Put"]
    E --> B
    C -->|data| F["write message"]
    F --> G{"err?"}
    G -->|yes| H(["return error"])
    G -->|no| I["pool.Put"]
    I --> B
```

## pingLoop

Private method - sends periodic ping frames to keep connections alive.

```mermaid
flowchart TB
    A(["pingLoop"]) --> B["new ticker"]
    B --> C["select"]
    C --> D{"event?"}
    D -->|ctx.Done| E(["return"])
    D -->|ticker| F["send ping"]
    F --> C
```

## handleTextMessage

Private method - routes text messages to appropriate handlers based on type.

```mermaid
flowchart TB
    A(["handleTextMessage"]) --> B["json.Unmarshal"]
    B --> C{"err?"}
    C -->|yes| D(["return"])
    C -->|no| E{"msg.Type?"}
    E -->|prompt| F["handle prompt"]
    E -->|steer| G["handle steer"]
    E -->|status| H["handle status"]
    E -->|other| I(["return"])
    F --> I
    G --> I
    H --> I
```

### Message Types

| Type | Description |
|------|-------------|
| `prompt` | Submits task to agent via `agentManager.GetAgent` |
| `steer` | Stops agent via `agentManager.GetExistingAgent.Stop()` |
| `status` | Gets agent status via `agentManager.GetOrCreateAgentForUser` |

## ActiveConns

Returns current number of active WebSocket connections.

```mermaid
flowchart LR
    A(["ActiveConns"]) --> B["atomic.LoadInt32"]
    B --> C(["return count"])
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/gorilla/websocket` | WebSocket protocol |
| `github.com/google/uuid` | UUID generation |
| `github.com/rs/zerolog` | Structured logging |
| `internal/agent` | Agent Task struct |
| `internal/ctx` | Context utilities |

**File Location:** [`internal/websocket/handler.go`](../../internal/websocket/handler.go)
