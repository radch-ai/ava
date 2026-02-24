# cmd/gateway (cmd/gateway/main.go)

 
Gateway application entry point. Initializes configuration, logger, model registry, Redis client, and starts the HTTP/WebSocket server.
 
## Overview
 
```mermaid
flowchart TB
    Start([main]) --> LoadCfg["config.Load()
    📄 ../config/README.md"]
    LoadCfg --> HValidate

    subgraph SGValidate [" "]
        HValidate["**cfg.Validate() → error**"]
        HValidate --> CheckAddr["check ServerAddr"]
        CheckAddr --> CheckProd{isProduction?}
        CheckProd -->|Yes| CheckSecrets["check secrets"]
        CheckSecrets --> CheckURL["validate Auth URL"]
        CheckProd -->|No| CheckURL
        CheckURL -->|ok| ValidNil["return nil"]
        CheckURL -->|error| ValidErr["return error"]
    end

    ValidErr --> Exit1([os.Exit(1)])
    ValidNil --> ParseLevel["zerolog.ParseLevel()"]
    ParseLevel -->|error| Exit2([os.Exit(1)])
    ParseLevel -->|ok| CreateLogger["zerolog.New()"]
    CreateLogger --> LoadModels["registry.LoadFromYAML()
    📄 ../internal/llm-proxy/models.md"]
    LoadModels --> InitRedis["redis.NewClient()"]
    InitRedis --> CreateCache["NewRedisCache()
    📄 ../internal/server/README.md"]
    CreateCache --> CreateAuthMW["NewAuthMiddleware()
    📄 ../internal/server/README.md"]
    CreateAuthMW --> CreateWS["websocket.NewHandler()
    📄 ../internal/websocket.md"]
    CreateWS --> CreateInternal["NewInternalRouter()"]
    CreateInternal --> CreateServer["server.New()"]
    CreateServer --> StartServer["srv.Start()"]
    StartServer -->|error| Exit3([os.Exit(1)])
    StartServer -->|ok| Running([Server Running])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Running mainFn
    class Exit1,Exit2,Exit3 errorFlow
    class LoadCfg,LoadModels,CreateCache,CreateAuthMW,CreateWS localPkgs
    class HValidate,ParseLevel,CreateLogger,InitRedis,CreateInternal,CreateServer,StartServer internalFn
```

## Variables

| Variable | Type | Description |
|----------|------|-------------|
| `cfg` | `*config.Config` | Loaded configuration |
| `logger` | `zerolog.Logger` | Configured logger |
| `registry` | `*models.Registry` | LLM model registry |
| `redisClient` | `*redis.Client` | Redis client (optional) |
| `cache` | `server.Cache` | Session cache adapter |
| `authMW` | `*server.AuthMiddleware` | Auth middleware |
| `wsHandler` | `*websocket.Handler` | WebSocket handler |
| `internalRouter` | `*server.InternalRouter` | Internal endpoints |
| `srv` | `*server.Server` | HTTP server |

## Error Handling

| Error Type | Handler | Exit Code / Behavior |
|------------|---------|---------------------|
| Config load error | `fmt.Fprintf(os.Stderr)` | Exit 1 |
| Config validation error | `fmt.Fprintf(os.Stderr)` | Exit 1 |
| Invalid log level | `fmt.Fprintf(os.Stderr)` | Exit 1 |
| Server start error | `logger.Error()` | Exit 1 |

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `GATEWAY_MODEL_CONFIG` | No | `config/models.yaml` | Model config path |

See [Config README](../config/README.md) for full environment variable list.

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

## Test Coverage

This package has no unit tests. Integration tests cover the server startup.

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/rs/zerolog` | Structured logging |
| `github.com/redis/go-redis/v9` | Redis client |
| `github.com/nevaria-ai/gateway/config` | Configuration |
| `github.com/nevaria-ai/gateway/internal/llm-proxy/models` | Model registry |
| `github.com/nevaria-ai/gateway/internal/server` | HTTP server |
| `github.com/nevaria-ai/gateway/internal/websocket` | WebSocket handler |

**File Location:** [`cmd/gateway/main.go`](../../cmd/gateway/main.go)
