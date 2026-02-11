# Server

The HTTP server manages the gateway's web interface, providing both health check endpoints and WebSocket gateway integration. It implements graceful shutdown with signal handling and includes production-ready timeout configurations.

## Overview

- **HTTP Server**: Standard Go HTTP server with configured timeouts
- **Health Check**: `/health` endpoint for readiness/liveness probes
- **WebSocket Integration**: `/ws` endpoint delegates to WebSocket handler
- **Graceful Shutdown**: Handles SIGINT/SIGTERM with 30-second shutdown timeout
- **Signal Handling**: Responds to OS signals for clean shutdown
- **Logging**: Structured logging with service name and version

## Architecture

The server consists of a single `Server` struct that wraps `http.Server`:

```go
type Server struct {
    httpServer *http.Server
    logger     zerolog.Logger
    wsHandler  *websocket.Handler
    shutdownCh chan struct{} // Closed when shutdown is requested
}
```

## Quick Start

```go
import (
    "github.com/nevaria-ai/gateway/internal/server"
    "github.com/nevaria-ai/gateway/internal/websocket"
    "github.com/rs/zerolog"
)

// Create logger
logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

// Create WebSocket handler
wsHandler := websocket.NewHandler(logger, []string{"*"}, 10000)

// Create server
srv := server.New(":8080", logger, wsHandler)

// Start server (blocking until shutdown)
if err := srv.Start(); err != nil {
    log.Fatal(err)
}
```

## Endpoints

| Endpoint | Methods | Description |
|----------|---------|-------------|
| `/health` | GET, HEAD | Health check endpoint returning "OK" |
| `/ws` | WebSocket | WebSocket upgrade endpoint |

## Documentation

- [Server Reference](server.md) - Server struct, New, Start, Shutdown methods

## Configuration

The server uses these default timeouts:

| Timeout | Value | Purpose |
|---------|-------|---------|
| ReadTimeout | 15s | Maximum time to read request |
| WriteTimeout | 15s | Maximum time to write response |
| IdleTimeout | 60s | Maximum idle time for connections |
| Shutdown Timeout | 30s | Maximum time to wait for graceful shutdown |

## Related Documentation

- [WebSocket Handler](../websocket/README.md) - WebSocket communication protocol
- [Agent Framework](../agent/README.md) - Agent runtime and execution
- [LLM Clients](../llm/README.md) - LLM provider integrations
