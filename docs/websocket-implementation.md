# WebSocket Implementation

## Overview

The gateway implements a production-ready WebSocket handler with connection limiting, single-writer pattern, and memory optimization using `sync.Pool`.

## Handler

The `Handler` type manages WebSocket connections with connection limiting and origin validation.

### Configuration

```go
handler := websocket.NewHandler(
    logger,
    []string{"https://example.com", "https://app.example.com"},
    10000, // max connections
)
```

### Connection Limiting

When `maxConns > 0`, the handler uses a buffered channel as a semaphore to limit concurrent connections:

```go
var sem chan struct{}
if maxConns > 0 {
    sem = make(chan struct{}, maxConns)
}
```

Connections exceeding the limit receive `503 Service Unavailable`.

### Origin Validation

The `CheckOrigin` function validates the `Origin` header against a whitelist:

- Empty origins are allowed (non-browser clients)
- `"*"` allows all origins (development only)
- Otherwise, exact match against allowed origins list

## Single-Writer Pattern

WebSocket connections are not safe for concurrent writes. The handler uses a single writer goroutine with a channel to serialize all writes.

### Writer Goroutine

```go
// Channel for serializing all writes
writeCh := make(chan *wsMessage, 8)

// Start single writer goroutine
go h.writer(ctx, conn, writeCh, logCtx, writerErrCh)
```

All write operations (echo responses, ping frames, close frames) go through this channel.

### Message Flow

```
Reader Loop          writeCh           Writer Goroutine
    |                                      ^
    | -- wsMessage ----------------------> |
    |                                      |
    |                                      v
                                         Conn.WriteMessage()
```

## Memory Optimization

### sync.Pool for wsMessage

Message structs are pooled to reduce GC pressure:

```go
var wsMsgPool = sync.Pool{
    New: func() interface{} {
        return &wsMessage{}
    },
}
```

**Usage pattern:**

```go
// Get from pool
msg := wsMsgPool.Get().(*wsMessage)
msg.messageType = websocket.TextMessage
msg.data = message

// After use, reset and return to pool
msg.messageType = 0
msg.data = nil
wsMsgPool.Put(msg)
```

### Buffer Sizing

| Constant | Value | Purpose |
|----------|-------|---------|
| `defaultBufferSize` | 8192 (8KB) | Per-connection read/write buffers |
| `maxMessageSize` | 65536 (64KB) | Maximum message size limit |

**Memory calculation:** With 100K concurrent connections, buffers use ~1.6GB RAM (16KB per connection).

## Connection Lifecycle

### 1. Connection Establishment

```
HTTP Request -> Origin Check -> Limit Check -> Upgrade -> Generate UUID
```

### 2. Active Connection

```
Reader Loop              Ping Loop
    |                        |
    v                        v
Read Message           Send Ping (30s)
    |                        |
    v                        v
Send to writeCh <----- writeCh
```

### 3. Connection Closure

```
Context Cancelled -> Try Send Close Frame -> Close Connection
```

## Ping/Pong

The handler sends ping frames every 30 seconds to detect dead connections:

```go
const pingInterval = 30 * time.Second

func (h *Handler) pingLoop(ctx context.Context, writeCh chan<- *wsMessage, logger zerolog.Logger) {
    ticker := time.NewTicker(pingInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            msg := wsMsgPool.Get().(*wsMessage)
            msg.messageType = websocket.PingMessage
            msg.data = nil
            select {
            case writeCh <- msg:
            case <-ctx.Done():
                wsMsgPool.Put(msg)
                return
            }
        }
    }
}
```

Pong responses reset the read deadline:

```go
conn.SetPongHandler(func(appData string) error {
    conn.SetReadDeadline(time.Now().Add(readTimeout))
    return nil
})
```

## Timeouts

| Timeout | Duration | Purpose |
|---------|----------|---------|
| `readTimeout` | 60s | Close idle connections |
| `writeTimeout` | 10s | Prevent write blocking |
| `pingInterval` | 30s | Keep-alive detection |

## Context Cancellation

The handler uses `context.Context` for cooperative shutdown:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Watch for client disconnect
go func() {
    <-r.Context().Done()
    cancel()
}()
```

All goroutines (reader, writer, ping) watch `ctx.Done()` and exit gracefully.

## Error Handling

### Unexpected Close Errors

Only log unexpected close errors:

```go
if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
    logCtx.Error().Err(err).Msg("unexpected close")
}
```

### Writer Errors

Writer goroutine reports errors via a buffered channel:

```go
writerErrCh := make(chan error, 1)

// In reader loop
case err := <-writerErrCh:
    logCtx.Error().Err(err).Msg("writer failed")
    cancel()
    return
```

## Testing

See `internal/websocket/handler_test.go` for connection tests and `internal/websocket/handler_bench_test.go` for benchmarks.

### Running Benchmarks

```bash
go test -bench=. -benchmem ./internal/websocket/
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GATEWAY_ALLOWED_ORIGINS` | (none) | Comma-separated origin whitelist |
| `GATEWAY_MAX_CONNS` | 0 (unlimited) | Maximum concurrent connections |

**Note:** Empty `GATEWAY_ALLOWED_ORIGINS` denies all connections. Use `"*"` for development only.
