# Gateway Foundation

## Overview

The Nevaria Gateway is a multi-tenant WebSocket gateway for AI agent tool routing.

## Project Structure

```
gateway/
├── cmd/gateway/         # Service entry point
├── internal/
│   ├── config/          # Environment-based configuration
│   ├── project/         # Generated project details
│   ├── server/          # HTTP server with graceful shutdown
│   └── websocket/       # WebSocket handler
├── scripts/             # Code generation scripts
├── docs/                # Project documentation
└── .claude/             # AI assistant context
```

## Design Patterns

### Single-Writer Pattern

Only one goroutine writes to each WebSocket connection. This prevents concurrent write panics and is the recommended approach for gorilla/websocket.

### Connection Limiting

Uses a semaphore pattern (buffered channel) to limit concurrent connections. When the limit is reached, new connections receive HTTP 503.

### Graceful Shutdown

All goroutines watch `context.Context` for cooperative shutdown:
- Reader exits on context cancel
- Writer drains channel before exiting
- Ping loop stops immediately on cancel
- Connection sends close frame when possible

## Protocol

The gateway implements the WebSocket protocol for real-time bidirectional communication:

- **Upgrade:** HTTP GET `/ws` upgrades to WebSocket
- **Origin Validation:** Whitelist-based for browser security
- **Keep-Alive:** Ping/pong frames every 30 seconds
- **Timeouts:** 60s read timeout, 10s write timeout

## Technology Stack

| Component | Choice | Purpose |
|-----------|--------|---------|
| Language | Go 1.21+ | Performance, concurrency |
| WebSocket | gorilla/websocket | Standard Go WebSocket lib |
| Logging | zerolog | Structured, zero-allocation |
| UUID | google/uuid | Connection IDs |
| Testing | testify | Assertions, test suite |
