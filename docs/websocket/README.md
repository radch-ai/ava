# WebSocket Gateway

The WebSocket gateway provides real-time bidirectional communication between clients and agents. It manages connection lifecycle, agent assignment, event streaming, and includes performance optimization features like connection pooling, rate limiting, and graceful shutdown.

## Overview

- **Real-time Communication**: WebSocket connections for instant message exchange
- **Agent Management**: Automatic agent assignment and per-connection state
- **Event Streaming**: Live event broadcasting for real-time updates
- **Connection Management**: Connection limiting, ping/pong keepalive, and graceful shutdown
- **Security**: Origin validation and connection limits
- **Performance**: Optimized for high concurrency with memory-efficient design

## Architecture

The gateway consists of three main components:

| Component | Description |
|-----------|-------------|
| [Handler](handler.md) | WebSocket protocol handling, connection lifecycle, message routing |
| [Agent Manager](agent_manager.md) | Per-connection agent lifecycle and management |
| [Protocol](protocol.md) | Message formats, event types, and communication flow |

## Quick Start

```go
import "github.com/nevaria-ai/gateway/internal/websocket"

// Create handler with origin validation and connection limits
handler := websocket.NewHandler(logger, []string{"*"}, 10000)

// Register with HTTP server
http.HandleFunc("/ws", handler.HandleWebSocket)
```

## Documentation

- [Handler Reference](handler.md) - Handler struct, Upgrader, ServeHTTP, writer, forwardAgentEvents
- [Agent Manager](agent_manager.md) - AgentManager struct and GetOrCreateAgent
- [Protocol Specification](protocol.md) - Message types, event formats, communication flow
