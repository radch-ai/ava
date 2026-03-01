# Gateway Command

Gateway application entry point. Initializes configuration, logger, LLM model selector, Redis client, and starts HTTP/WebSocket server.

## Overview

```mermaid
flowchart TB
    Start([main]) --> LoadCfg["config.Load() → cfg, err"]
    LoadCfg -->|error| Exit1("Exit 1")
    LoadCfg -->|ok| ValidateCfg["cfg.Validate() → err"]
    ValidateCfg -->|error| Exit1
    ValidateCfg -->|ok| ParseLevel["zerolog.ParseLevel(cfg.LogLevel) → logLevel, err"]
    ParseLevel -->|error| Exit2("Exit 1")
    ParseLevel -->|ok| SetLevel["zerolog.SetGlobalLevel(logLevel)"]
    SetLevel --> CreateLogger["zerolog.New(os.Stderr) → logger"]
    CreateLogger --> LoadModels["config.InitLLMModels('models.json') → modelsCfg, err"]
    LoadModels -->|error| ExitLLM["Exit 1 - models.json required"]
    LoadModels -->|ok| CreateSelector["llm.NewModelSelector(modelsCfg) → modelSelector"]
    CreateSelector --> CheckOrigins["cfg.AllowedOrigins (warn if empty)"]
    CheckOrigins --> CheckConns["cfg.MaxConns (default 10000)"]
    CheckConns --> CreateWS["websocket.NewHandler(logger, allowedOrigins, maxConns, nil) → wsHandler"]
    CreateWS --> InitRedis["redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword}) → redisClient"]
    InitRedis --> CreateCache["server.NewRedisCache(redisClient) → cache"]
    CreateCache --> CreateAuthMW["server.NewAuthMiddleware(cfg.ZetidalURL, cache, cfg.AuthCacheTTL) → authMW"]
    CreateAuthMW --> CreateServer["server.New(cfg.ServerAddr, logger, wsHandler, authMW) → srv"]
    CreateServer --> StartServer["srv.Start() → err"]
    StartServer -->|error| Exit3("Exit 1")
    StartServer -->|ok| Running("Server Running")

    click LoadCfg href "../config/README.md"
    click ValidateCfg href "../config/README.md"
    click LoadModels href "../config/models.md"
    click CreateCache href "../internal/server/README.md"
    click CreateAuthMW href "../internal/server/README.md"
    click CreateWS href "../internal/websocket.md"
    click CreateServer href "../internal/server/README.md"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Running mainFn
    class Exit1,Exit2,Exit3,ExitLLM errorFlow
    class LoadCfg,ValidateCfg,LoadModels,CreateSelector,CheckOrigins,CheckConns,CreateCache,CreateAuthMW,CreateWS,CreateServer,StartServer localPkgs
```

## Design Notes

### Default Connection Limit

**Q: Why default to 10000 connections?**

| Aspect | Description |
|--------|-------------|
| Context | WebSocket connection limits |
| Options considered | 1000, 10000, unlimited |
| Rationale | 10000 provides reasonable capacity while preventing resource exhaustion |

**Current approach is recommended because:**
- Handles typical traffic spikes
- Leaves room for other processes
- Prevents accidental resource exhaustion
- Can be configured for specific deployments

### Warning on Missing Origins

**Q: Why warn instead of fail on missing GATEWAY_ALLOWED_ORIGINS?**

| Aspect | Description |
|--------|-------------|
| Context | CORS configuration |
| Options considered | Fail, Warn + allow all, Deny all |
| Rationale | Development convenience with production safety via warnings |

**Current approach is recommended because:**
- Developers can start quickly locally
- Warning ensures production deployments are configured
- Consistent with security best practices
- Easy to fix when warning appears in logs

### Required LLM Configuration

**Q: Why fail if models.json is missing?**

| Aspect | Description |
|--------|-------------|
| Context | LLM configuration loading |
| Options considered | Fail fast, continue without LLM |
| Rationale | Gateway requires LLM support to function - agents cannot operate without models |

**Current approach is recommended because:**
- Gateway's primary purpose is LLM agent orchestration
- Missing models config indicates deployment error
- Failing fast prevents confusing runtime errors
- Clear error message helps operators fix the issue

### Nil AgentManagerOptions

**Q: Why pass nil for AgentManagerOptions to WebSocket handler?**

| Aspect | Description |
|--------|-------------|
| Context | AgentManager needs QuotaChatModel for LLM quota enforcement |
| Options considered | Pass nil, Create mock, Wire full LLM client |
| Rationale | LLM integration incomplete - quota enforcement not yet implemented |

**Current approach is temporary:**
- AgentManager accepts nil opts and operates without quota enforcement
- QuotaChatModel will be wired once LLM client integration is complete
- Agents can still run but without usage tracking
- TODO: Wire QuotaChatModel from models config when ready

## Test Coverage

This package has no unit tests. Integration tests cover the server startup.

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/rs/zerolog` | Structured logging |
| `github.com/redis/go-redis/v9` | Redis client |
| `github.com/nevaria-ai/gateway/config` | Configuration |
| `github.com/nevaria-ai/gateway/internal/llm` | LLM model selector |
| `github.com/nevaria-ai/gateway/internal/server` | HTTP server |
| `github.com/nevaria-ai/gateway/internal/websocket` | WebSocket handler |

**File Location:** [`cmd/gateway/main.go`](https://github.com/nevaria-ai/gateway/blob/main/cmd/gateway/main.go)
