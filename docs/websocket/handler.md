# Handler

The `Handler` manages WebSocket connections with connection limiting and origin validation.

## Struct Definition

```go
type Handler struct {
    logger       zerolog.Logger
    upgrader     websocket.Upgrader
    connSem      chan struct{} // Semaphore for connection limiting
    maxConns     int
    activeConns  int32         // atomic counter
    agentManager *AgentManager
}
```

## Constructor

```go
func NewHandler(logger zerolog.Logger, allowedOrigins []string, maxConns int) *Handler
```

Creates a new WebSocket handler with connection limiting.

**Parameters:**
- `logger` - zerolog logger for structured logging
- `allowedOrigins` - List of allowed origin strings (use `["*"]` to allow all)
- `maxConns` - Maximum concurrent connections (0 for unlimited)

## Handler Methods

### HandleWebSocket

```go
func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request)
```

Handles an incoming WebSocket connection upgrade request.

**Flow:**
1. Acquires semaphore slot (if connection limiting enabled)
2. Increments active connection counter
3. Upgrades HTTP to WebSocket
4. Generates connection ID and session ID
5. Gets or creates agent for session
6. Subscribes to agent events
7. Starts writer, ping, and event forwarding goroutines
8. Enters message loop for client messages

### ActiveConns

```go
func (h *Handler) ActiveConns() int
```

Returns the current number of active WebSocket connections using atomic load.

## Internal Goroutines

### writer

```go
func (h *Handler) writer(ctx context.Context, conn *websocket.Conn, writeCh <-chan *wsMessage, logger zerolog.Logger, writerErrCh chan<- error)
```

Serializes all writes to the WebSocket connection (single-writer pattern). Prevents concurrent write panics by ensuring only one goroutine writes to the connection at a time.

**Features:**
- Writes messages from `writeCh` channel
- Returns messages to `sync.Pool` after use
- Reports errors via `writerErrCh`
- Respects context cancellation

### pingLoop

```go
func (h *Handler) pingLoop(ctx context.Context, writeCh chan<- *wsMessage, logger zerolog.Logger)
```

Sends periodic ping frames to keep connections alive and detect dead connections.

**Configuration:**
- Interval: 30 seconds (`pingInterval` constant)
- Uses `sync.Pool` for ping messages
- Timeout on send: 10 seconds (`writeTimeout`)

### forwardAgentEvents

```go
func (h *Handler) forwardAgentEvents(ctx context.Context, eventCh <-chan agent.AgentEvent, writeCh chan<- *wsMessage, logger zerolog.Logger)
```

Forwards agent events to the WebSocket client. Converts internal agent events to WebSocket event format.

**Features:**
- Subscribes to agent event channel
- Converts events using `agentEventToWSEvent`
- Uses `sync.Pool` for message structs
- 100ms timeout for send attempts

## Message Handling

### handleTextMessage

```go
func (h *Handler) handleTextMessage(ctx context.Context, data []byte, agentInstance *agent.Agent, writeCh chan<- *wsMessage, logger zerolog.Logger)
```

Handles incoming text messages from the WebSocket client.

**Message Format:**
```json
{
    "type": "prompt",
    "text": "Hello, agent!"
}
```

**Supported Types:**
- `prompt` - Sends text to agent via `agentInstance.Prompt()`

### agentEventToWSEvent

```go
func (h *Handler) agentEventToWSEvent(event agent.AgentEvent) *wsEvent
```

Converts internal agent events to WebSocket events. See [protocol.md](protocol.md) for event type mappings.

## Connection Management

### Origin Validation

```go
func checkOrigin(r *http.Request, allowedOrigins []string) bool
```

Validates the `Origin` header against allowed origins.

**Rules:**
- Empty `Origin` header: allowed (non-browser clients)
- `"*"` in allowed list: allows all origins
- Exact match: allowed

### Connection Limiting

Uses buffered channel as semaphore:
```go
if h.connSem != nil {
    select {
    case h.connSem <- struct{}{}:
        // Got slot, continue
        defer func() { <-h.connSem }()
    default:
        // At capacity - return 503
    }
}
```

### Graceful Shutdown

- Context cancellation propagates to all goroutines
- Writer respects context and exits cleanly
- Ping loop exits on context cancellation
- Attempts to send close frame before closing

## Performance Optimizations

### sync.Pool for Messages

```go
var wsMsgPool = sync.Pool{
    New: func() interface{} {
        return &wsMessage{}
    },
}
```

Reduces GC pressure by reusing message structs. Always returned to pool after use:
```go
msg.messageType = 0  // Reset
msg.data = nil
wsMsgPool.Put(msg)
```

### Single Writer Pattern

All WebSocket writes go through a single goroutine to prevent concurrent write panics. Uses buffered channel (8 messages) for queuing.

### Buffer Configuration

| Constant | Value | Purpose |
|----------|-------|---------|
| `defaultBufferSize` | 8192 (8KB) | Read/write buffer size |
| `maxMessageSize` | 65536 (64KB) | Maximum message size |
| `readTimeout` | 60s | Read deadline |
| `writeTimeout` | 10s | Write deadline |
| `pingInterval` | 30s | Ping frame interval |

## Related

- [Agent Manager](agent_manager.md) - Per-connection agent lifecycle
- [Protocol](protocol.md) - Event formats and message types
