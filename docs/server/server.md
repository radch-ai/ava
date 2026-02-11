# Server

The `Server` type manages the HTTP server lifecycle, WebSocket handler integration, and graceful shutdown.

## Type Definition

```go
type Server struct {
    httpServer *http.Server
    logger     zerolog.Logger
    wsHandler  *websocket.Handler
    shutdownCh chan struct{} // Closed when shutdown is requested
}
```

| Field | Type | Description |
|-------|------|-------------|
| `httpServer` | `*http.Server` | Underlying HTTP server with configured timeouts |
| `logger` | `zerolog.Logger` | Structured logger for server events |
| `wsHandler` | `*websocket.Handler` | WebSocket handler for `/ws` endpoint |
| `shutdownCh` | `chan struct{}` | Closed when shutdown is requested |

## Constructor

### New

```go
func New(addr string, logger zerolog.Logger, wsHandler *websocket.Handler) *Server
```

Creates a new `Server` with the given address, logger, and WebSocket handler.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `addr` | `string` | Address to listen on (e.g., `":8080"` or `"0.0.0.0:8080"`) |
| `logger` | `zerolog.Logger` | Structured logger instance |
| `wsHandler` | `*websocket.Handler` | WebSocket handler for `/ws` endpoint |

**Returns:** `*Server` - Configured server instance ready to start

**Default Configuration:**

| Setting | Value |
|---------|-------|
| ReadTimeout | 15 seconds |
| WriteTimeout | 15 seconds |
| IdleTimeout | 60 seconds |

**Example:**

```go
logger := zerolog.New(os.Stdout).With().Timestamp().Logger()
wsHandler := websocket.NewHandler(logger, []string{"*"}, 10000)

srv := server.New(":8080", logger, wsHandler)
```

## Methods

### Start

```go
func (s *Server) Start() error
```

Starts the HTTP server and blocks until shutdown is complete or an error occurs.

**Behavior:**

1. Logs server startup with service name, version, and address
2. Starts HTTP server in a goroutine
3. Waits for one of three conditions:
   - Server startup error
   - Shutdown signal (SIGINT or SIGTERM)
   - Manual shutdown request via `shutdownCh`
4. Initiates graceful shutdown with 30-second timeout
5. Returns any error that occurred during startup or shutdown

**Signals Handled:**

- `SIGINT` (Ctrl+C)
- `SIGTERM` (termination signal)

**Shutdown Timeout:** 30 seconds

**Returns:** `error` - `nil` on clean shutdown, error if startup failed or shutdown timed out

**Example:**

```go
// Start server (blocks until shutdown)
if err := srv.Start(); err != nil {
    log.Fatalf("Server error: %v", err)
}
```

### Shutdown

```go
func (s *Server) Shutdown(ctx context.Context) error
```

Gracefully shuts down the HTTP server.

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | Context with timeout for shutdown |

**Behavior:**

1. Closes `shutdownCh` to signal `Start()` that shutdown is requested
2. Calls `http.Server.Shutdown()` to gracefully close connections
3. Waits for active connections to complete (respecting context timeout)

**Returns:** `error` - `nil` on successful shutdown, error if context expires

**Example:**

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := srv.Shutdown(ctx); err != nil {
    log.Printf("Shutdown error: %v", err)
}
```

## HTTP Endpoints

### /health

Health check endpoint for readiness/liveness probes.

**Methods:** GET, HEAD

**Response:**

- Status Code: `200 OK`
- Content-Type: `text/plain`
- Body: `OK`

**Behavior:**

- Returns `200 OK` for GET and HEAD requests
- Returns `405 Method Not Allowed` for other HTTP methods

**Example:**

```bash
$ curl http://localhost:8080/health
OK
```

### /ws

WebSocket upgrade endpoint. Delegates to the `websocket.Handler`.

**Methods:** WebSocket Upgrade

**Behavior:**

- Upgrades HTTP connection to WebSocket protocol
- Connection handling is delegated to the `wsHandler`

See [WebSocket Handler](../websocket/README.md) for protocol details.

## Server Lifecycle

```
                    New()
                      |
                      v
                 +--------+
                 | Ready  |
                 +--------+
                      |
                      | Start()
                      v
    +----------------------------------+
    |           Running               |
    |  - Listening on addr            |
    |  - Handling requests            |
    |  - Waiting for signals          |
    +----------------------------------+
         |                |           |
         | Shutdown()     | Signal    | Error
         |                |           |
         v                v           v
    +------------------+     +-------------+
    |  Graceful        |     |  Failed     |
    |  Shutdown        |     |  (returns   |
    |  (30s timeout)   |     |   error)    |
    +------------------+     +-------------+
              |
              v
    +------------------+
    |  Stopped         |
    |  (returns nil)   |
    +------------------+
```

## Signal Handling

The server responds to OS signals for graceful shutdown:

```go
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
```

When a signal is received:

1. Server logs the signal received
2. Graceful shutdown begins
3. Active connections are allowed up to 30 seconds to complete
4. Server exits cleanly

## Error Handling

### Startup Errors

Errors during server startup return immediately from `Start()`:

```go
err := srv.Start()
// Returns error if:
// - Address already in use
// - Permission denied for privileged port
// - Invalid address format
```

### Shutdown Errors

Shutdown errors occur when:

- Context timeout expires before connections close
- Server encounters error during shutdown process

```go
ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
defer cancel()

if err := srv.Shutdown(ctx); err != nil {
    // Handle shutdown timeout or error
}
```

### Connection Errors

The server logs connection errors but continues running:

- Failed connection attempts return `http.ErrServerClosed` on shutdown
- Individual request errors do not stop the server

## Best Practices

### 1. Always Handle Shutdown

```go
srv := server.New(":8080", logger, wsHandler)

// Start in goroutine for custom signal handling
go func() {
    if err := srv.Start(); err != nil && err != http.ErrServerClosed {
        log.Printf("Server error: %v", err)
    }
}()

// Your shutdown logic here
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
srv.Shutdown(ctx)
```

### 2. Use Appropriate Addresses

```go
// Listen on all interfaces
srv := server.New("0.0.0.0:8080", logger, wsHandler)

// Listen on localhost only
srv := server.New("localhost:8080", logger, wsHandler)

// Listen on specific interface
srv := server.New("192.168.1.10:8080", logger, wsHandler)
```

### 3. Set Reasonable Shutdown Timeouts

```go
// Long-running requests need more time
ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()
srv.Shutdown(ctx)
```

### 4. Check Health Endpoint Status

```go
// For readiness probes
resp, err := http.Get("http://localhost:8080/health")
if err != nil || resp.StatusCode != http.StatusOK {
    // Service not ready
}
```

## Testing

The server package includes comprehensive tests:

- `TestServerStarts` - Server startup and basic functionality
- `TestServerHealthEndpoint` - Health endpoint with various HTTP methods
- `TestServerWebSocketEndpoint` - WebSocket connection and messaging
- `TestServerGracefulShutdown` - Graceful shutdown behavior
- `TestServerStartupFailure` - Error handling for startup failures
- `TestServerWithCustomTimeouts` - Timeout configuration verification
- `TestNewServer` - Constructor validation
- `TestServerInvalidPath` - 404 handling for unknown paths
- `TestServerWithRealHTTPClient` - Integration testing
- `TestServerGracefulShutdownWithWebSocket` - Shutdown with active WebSocket connections

## Related Documentation

- [WebSocket Handler](../websocket/README.md) - WebSocket protocol and handler
- [Agent Manager](../websocket/agent_manager.md) - Agent lifecycle management
- [WebSocket Protocol](../websocket/protocol.md) - Message formats
