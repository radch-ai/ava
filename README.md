# Gateway

WebSocket gateway for Nevaria agent tool execution.

## About

Part of the Nevaria platform. This service provides real-time WebSocket communication routing between AI agents and tools in a multi-tenant environment.

## Architecture

The Gateway is deployed as a single shared multi-tenant service. It handles:

- WebSocket connections from agent devices
- Tool invocation routing (cloud-hosted and device-relayed)
- Authentication and session management
- Message protocol handling

## Development

### Prerequisites

- Go 1.21+
- Access to `../project-details/` for dynamic configuration

### Build

```bash
make build
```

Binary output: `bin/nevaria-gateway`

### Run

```bash
make run
```

### Test

```bash
make test
```

### Configuration

Project naming and module paths are dynamically generated from the parent `project-details` repository. After updating project details:

```bash
make generate
```

Runtime configuration via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| GATEWAY_ADDR | :8080 | Server bind address |
| GATEWAY_LOG_LEVEL | info | Log level (debug, info, warn, error) |
| GATEWAY_ALLOWED_ORIGINS | deny all | Comma-separated origin whitelist |
| GATEWAY_MAX_CONNS | 0 | Maximum concurrent connections (0 = unlimited) |

**Note:** Empty `GATEWAY_ALLOWED_ORIGINS` denies all browser connections. Use `"*"` for development only.

## API

### Health Check

```
GET /health
```

Returns `200 OK` with body `OK` for load balancer health checks.

### WebSocket

```
GET /ws
```

Upgrades HTTP to WebSocket connection. Requires authentication (future).

## Documentation

All documentation is consolidated in the `docs/` directory for easy navigation:

### Core Package Documentation

- `docs/agent.md` - Agent framework and API reference
  - Core types, state management, and tool execution
  - Event system and message handling
  - Tool registry and usage examples
  - Configuration and performance considerations

- `docs/llm.md` - LLM client interfaces and providers
  - Unified interface for multiple providers
  - Provider-specific implementations:
    - OpenAI client with streaming support
    - Anthropic Claude client integration
    - Google Gemini client
  - Error handling and metrics collection

- `docs/websocket.md` - WebSocket handler and connection management
  - Connection handling and agent management
  - Message types and communication flow
  - Protocol specification and event formats
  - Security features and performance optimization

### Architecture and Design

- `docs/foundation.md` - Architecture and design patterns
- `docs/performance.md` - Benchmarks, scaling, and tuning

## Deployment

Deployed as containerized service. See internal deployment documentation for:
- Container registry locations
- Environment variable configuration
- Service discovery setup
