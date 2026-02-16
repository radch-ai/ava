# Architecture

The Nevaria Gateway is a WebSocket-based AI agent gateway with authentication, intelligent LLM proxy routing, quota management, and multi-provider support.

## System Overview

```mermaid
flowchart TB
    subgraph Clients
        WS[WebSocket Client]
    end

    subgraph Gateway["Nevaria Gateway"]
        HTTP[HTTP Server]
        WSHandler[WebSocket Handler]
        AuthMW[Auth Middleware]

        subgraph Agents
            AgentRuntime[Agent Runtime]
            AgentLoop[Tool Loop]
            ToolReg[Tool Registry]
        end

        subgraph LLMProxy["LLM Proxy"]
            ProxyHandler[Proxy Handler]
            Router[Model Router]
            Health[Health Checker]
            Quota[Quota Manager]
            Usage[Usage Tracker]
        end

        subgraph LLM["LLM Clients"]
            OpenAI[OpenAI Client]
            Anthropic[Anthropic Client]
            Google[Google Client]
        end

        subgraph Internal["Internal Router"]
            InternalRouter[/_internal/*]
            CacheInvalid[Cache Invalidation]
        end
    end

    subgraph External["External Services"]
        AuthService[Auth Service]
        Redis[(Redis)]
    end

    WS -->|WebSocket| HTTP
    HTTP --> AuthMW
    AuthMW --> WSHandler

    AuthMW -->|Session Validate| AuthService
    AuthMW -.->|Cache| Redis

    WSHandler --> AgentRuntime
    AgentRuntime --> AgentLoop
    AgentLoop --> ToolReg

    AgentRuntime -->|LLM Call| ProxyHandler
    ProxyHandler --> Router
    ProxyHandler --> Health
    ProxyHandler --> Quota

    Router --> OpenAI
    Router --> Anthropic
    Router --> Google

    Quota --> Redis
    Health --> Redis

    InternalRouter --> CacheInvalid
    CacheInvalid --> Redis
```

## Package Structure

```mermaid
flowchart TB
    Gateway[Gateway]

    Gateway --> CMD[cmd]
    Gateway --> Internal
    Gateway --> Config[config/]

    CMD --> Main[main.go]

    Internal --> Server[server/]
    Internal --> WebSocket[websocket/]
    Internal --> Agent[agent/]
    Internal --> LLM[llm/]
    Internal --> LLMProxy[llm-proxy/]
    Internal --> Auth[auth/]
    Internal --> Ctx[ctx/]
    Internal --> Project[project/]

    LLMProxy --> PConfig[config/]
    LLMProxy --> PHealth[health/]
    LLMProxy --> PModels[models/]
    LLMProxy --> PQuota[quota/]
    LLMProxy --> PRouter[router/]
    LLMProxy --> PUsage[usage/]

    Server --> Middleware[middleware/]
    Server --> Internal[internal/]
    Server --> Cache[redis_cache/]
```

## Component Overview

| Package | Purpose |
|---------|---------|
| [`config/`](config.md) | Environment-based configuration with validation |
| [`internal/server/`](server.md) | HTTP server, middleware, routing |
| [`internal/websocket/`](websocket.md) | WebSocket connection management |
| [`internal/agent/`](agent/README.md) | Agent runtime, state management, event system |
| [`internal/llm/`](llm.md) | LLM client factory for multiple providers |
| [`internal/llm-proxy/`](llm-proxy/README.md) | LLM proxy with quota, health, routing |
| [`internal/auth/`](auth.md) | Auth service client for session validation |
| [`internal/ctx/`](ctx.md) | Context utilities for user ID propagation |
| [`internal/project/`](project.md) | Project metadata and constants |

## Data Flows

| Flow | Description |
|------|-------------|
| WebSocket Connection | Auth validate → Agent created → Events stream |
| LLM Proxy Request | Quota check → Router → Health check → LLM call → Usage track |
| Agent Tool Loop | LLM stream → Extract tool calls → Execute → Add results → Repeat |
| Session Validation | Cookie extract → Cache check → Auth service call → User ID in context |
| Cache Invalidation | Internal webhook → Delete from Redis |

## Design Patterns

| Pattern | Description |
|---------|-------------|
| [Factory](../internal/llm/factory.go) | Creates provider-specific LLM clients |
| [Strategy](../internal/agent/loop.go) | Pluggable tool execution |
| [Observer](../internal/agent/agent.go) | Event subscription system |
| [Middleware](../internal/server/middleware.go) | HTTP request processing chain |
| [Singleton](../internal/llm-proxy/models/registry.go) | Global model registry |
| [Pool](../internal/websocket/handler.go) | `wsMessage` sync.Pool for efficiency |
| [Circuit Breaker](../internal/llm-proxy/health/circuit.go) | Provider health management |

## Technology Stack

| Component | Technology |
|-----------|------------|
| Language | Go 1.24 |
| WebSocket | gorilla/websocket |
| OpenAI SDK | openai/openai-go |
| Anthropic SDK | anthropics/anthropic-sdk-go |
| Google AI | google.golang.org/genai |
| Redis | go-redis/v9 |
| Logging | zerolog |
| Config | Environment variables |
| Router | gorilla/mux |

## Performance

### Connection Scaling
- Default buffer size: 8KB read + 8KB write per connection
- 100K connections: ~1.6GB RAM for buffers
- Connection limiting via semaphore pattern
- Horizontal scaling required for viral growth

### Optimization Techniques
- sync.Pool for WebSocket message structs
- Single-writer pattern for WebSocket writes
- Redis pipelining for quota operations
- Circuit breaker to fail fast on unhealthy providers
- Session caching to reduce auth service calls

## Protocol Notes

### WebSocket Messages

**Client to Server:**
```json
{
  "type": "prompt",
  "text": "user message here"
}
```

**Server to Client Events:**
```json
{
  "type": "connected",
  "data": {
    "session_id": "...",
    "agent_id": "...",
    "conn_id": "..."
  }
}
```

Event types: `connected`, `agent_start`, `agent_end`, `turn_start`, `turn_end`, `message_start`, `message_update`, `message_end`, `tool_start`, `tool_update`, `tool_end`

### Message Flow

1. Client connects via WebSocket upgrade
2. Auth middleware validates session cookie
3. Server sends `connected` event with session info
4. Client sends `prompt` messages
5. Agent emits streaming events back to client
6. Connection closes on client disconnect or error

### Internal Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/_internal/cache/delete` | POST | Bearer | Invalidate session cache |
| `/_internal/*` | * | Bearer | All internal endpoints protected |

## Related Documentation

- [Configuration](config.md) - Environment variables and setup
- [Server](server.md) - HTTP server and middleware
- [WebSocket](websocket.md) - WebSocket handler
- [Agent](agent/README.md) - Agent runtime
- [LLM](llm.md) - LLM client factory
- [LLM Proxy](llm-proxy/README.md) - Proxy routing and quota
- [Auth](auth.md) - Authentication client
