# Server

HTTP server with authentication middleware and WebSocket upgrade.

## Overview

```mermaid
flowchart TB
    Start(["Request"]) --> CheckAuth{authMW?}
    CheckAuth -->|yes| Authenticate["authMW.Authenticate"]
    CheckAuth -->|no| WSHandler["wsHandler.HandleWebSocket"]
    Authenticate -->|valid| WSHandler
    Authenticate -->|invalid| AuthErr(["401 Unauthorized"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,WSHandler mainFn
    class AuthErr errorFlow
```

**Auth Flow:**
1. Extract session cookie from request
2. Check cache (if available) for session → userID
3. Validate with auth service (if cache miss)
4. Add user_id to context
5. Call next handler (WebSocket upgrade)

## Structs

### Server

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Server` | `httpServer` | `*http.Server` | HTTP server |
| | `logger` | `zerolog.Logger` | Logger |
| | `wsHandler` | `*websocket.Handler` | WebSocket handler |
| | `authMW` | `*AuthMiddleware` | Auth middleware |
| | `shutdownCh` | `chan struct{}` | Shutdown signal |

### ServerConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ServerConfig` | `Addr` | `string` | Server address |
| | `AuthURL` | `string` | Auth service URL |
| | `AuthCacheTTL` | `time.Duration` | Auth cache TTL |

### AuthMiddleware

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AuthMiddleware` | `authClient` | `*AuthClient` | Auth service client |
| | `cache` | `AuthCache` | Session cache (can be nil) |
| | `cacheTTL` | `time.Duration` | Cache TTL |

### RedisCache

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RedisCache` | `client` | `*redis.Client` | Redis client |

## Interfaces

### AuthCache

```go
type AuthCache interface {
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key string, value string, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
}
```

## Simple Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `HTTPServer()` | `*http.Server` | Returns underlying HTTP server (for testing) |
| `WSHandler()` | `*websocket.Handler` | Returns WebSocket handler (for testing) |

## New

Creates a new Server with optional auth middleware.

```mermaid
flowchart TB
    Start(["New"]) --> NewMux["http.NewServeMux"]
    NewMux --> HandleHealth["HandleFunc /health"]
    HandleHealth --> CreateWSHandler["HandlerFunc for WebSocket"]
    CreateWSHandler --> CheckMW{authMW?}
    CheckMW -->|yes| WrapAuth["authMW.Authenticate"]
    CheckMW -->|no| UseHandler["use wsHandlerFunc"]
    WrapAuth --> HandleWS["mux.Handle /ws wrapped"]
    UseHandler --> HandleWS2["mux.Handle /ws"]
    HandleWS --> CreateServer["create Server"]
    HandleWS2 --> CreateServer
    CreateServer --> Done(["return srv"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Done mainFn
    class WrapAuth localPkgs
```

## NewWithRedis

Creates a Server with Redis-backed auth middleware (recommended for production).

```mermaid
flowchart TB
    Start(["NewWithRedis"]) --> NewCache["NewRedisCache"]
    NewCache --> NewMW["NewAuthMiddleware"]
    NewMW --> CallNew["New"]
    CallNew --> Done(["return srv"])

    click NewCache "#NewRedisCache" "Go to NewRedisCache"
    click NewMW "#NewAuthMiddleware" "Go to NewAuthMiddleware"
    click CallNew "#New" "Go to New"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Done mainFn
    class NewCache,NewMW,CallNew localPkgs
```

## NewAuthMiddleware

Creates a new authentication middleware with optional cache.

```mermaid
flowchart TB
    Start(["NewAuthMiddleware(authURL, cache, cacheTTL)"]) --> NewClient["NewAuthClient(authURL)"]
    NewClient --> CreateMW["&AuthMiddleware{authClient, cache, cacheTTL}"]
    CreateMW --> Done(["return mw"])

    click NewClient "auth.md#NewAuthClient" "Go to AuthClient"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Done mainFn
    class NewClient localPkgs
```

## Authenticate

Validates session cookie and adds user_id to context.

```mermaid
flowchart TB
    Start(["Authenticate(next)"]) --> GetCookie["r.Cookie(session)"]
    GetCookie --> CheckCookie{cookie?}
    CheckCookie -->|missing| WriteAuthErr(["writeAuthError"])
    CheckCookie -->|found| CheckCache{cache?}
    CheckCache -->|yes| CacheGet["cache.Get(session:token)"]
    CacheGet --> CheckCached{cachedUserID?}
    CheckCached -->|yes| SetCtx["ctx.WithUserID"]
    CheckCached -->|no| Validate
    CheckCache -->|no| Validate
    Validate["authClient.ValidateSession"]
    Validate -->|error| WriteAuthErr2(["writeAuthError"])
    Validate -->|ok| SetCtx
    SetCtx --> Next(["next.ServeHTTP"])

    click Validate "auth.md#ValidateSession" "Go to ValidateSession"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Next mainFn
    class Validate localPkgs
    class WriteAuthErr,WriteAuthErr2 errorFlow
```

## NewRedisCache

Creates a new Redis-backed cache.

```mermaid
flowchart TB
    Start(["NewRedisCache(client)"]) --> CreateCache["&RedisCache{client}"]
    CreateCache --> Done(["return cache"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Done mainFn
```

## RedisCache Methods

### Get

```mermaid
flowchart TB
    Start(["Get(ctx, key)"]) --> ClientGet["c.client.Get(ctx, key).Result()"]
    ClientGet --> CheckError{err?}
    CheckError -->|redis.Nil| ReturnNotFound(["return error: key not found"])
    CheckError -->|other| ReturnErr(["return error"])
    CheckError -->|no| ReturnVal(["return val, nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnVal mainFn
    class ReturnNotFound,ReturnErr errorFlow
```

### Set

```mermaid
flowchart TB
    Start(["Set(ctx, key, value, ttl)"]) --> ClientSet["c.client.Set(ctx, key, value, ttl).Err()"]
    ClientSet --> Return(["return err"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### Delete

```mermaid
flowchart TB
    Start(["Delete(ctx, key)"]) --> ClientDel["c.client.Del(ctx, key).Err()"]
    ClientDel --> Return(["return err"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## Start

Starts the HTTP server and handles graceful shutdown.

```mermaid
flowchart TB
    Start(["Start()"]) --> LogInfo["logger.Info()"]
    LogInfo --> CreateErrCh["make(chan error, 1)"]
    CreateErrCh --> GoListen["go s.httpServer.ListenAndServe()"]
    GoListen --> CreateSigCh["make(chan os.Signal, 1)"]
    CreateSigCh --> NotifySignal["signal.Notify(SIGINT, SIGTERM)"]
    NotifySignal --> Select

    Select{{"select"}}
    Select -->|errCh| ErrCh["err := <-errCh"]
    ErrCh --> LogError["logger.Error()"]
    LogError --> ReturnErr(["return err"])
    Select -->|sigChan| Sig["sig := <-sigChan"]
    Sig --> LogSignal["logger.Info()"]
    LogSignal --> CreateCtx
    Select -->|shutdownCh| ShutdownCh["<-s.shutdownCh"]
    ShutdownCh --> LogShutdown["logger.Info()"]
    LogShutdown --> CreateCtx

    CreateCtx["context.WithTimeout(30s)"]
    CreateCtx --> DoShutdown["s.httpServer.Shutdown(ctx)"]
    DoShutdown --> CheckShutdownErr{err?}
    CheckShutdownErr -->|yes| LogShutdownErr["logger.Error()"]
    LogShutdownErr --> ReturnShutdownErr(["return fmt.Errorf()"])
    CheckShutdownErr -->|no| LogStopped["logger.Info()"]
    LogStopped --> ReturnNil(["return nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnNil mainFn
    class ReturnErr,ReturnShutdownErr errorFlow
```

## Shutdown

Gracefully shuts down the HTTP server.

```mermaid
flowchart TB
    Start(["Shutdown(ctx)"]) --> CloseCh["close(s.shutdownCh)"]
    CloseCh --> ShutdownServer["s.httpServer.Shutdown(ctx)"]
    ShutdownServer --> Return(["return err"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## Design Notes

### Simple Auth Flow

**Q: Why remove sliding sessions?**

| Aspect | Description |
|--------|-------------|
| Context | Client handles token refresh via refresh tokens |
| Options considered | Gateway-managed sessions, client-managed sessions |
| Rationale | Simpler gateway - only verify and upgrade to WS |

**Current approach is recommended because:**
- Gateway only validates session authenticity
- Client handles all token/cookie management
- No response wrapper needed
- Cleaner separation of concerns

### Optional Auth Cache

**Q: Why use optional caching?**

| Aspect | Description |
|--------|-------------|
| Context | Reduce auth service load |
| Options considered | No caching, Redis caching, in-memory caching |
| Rationale | Optional Redis caching for production, no cache for testing |

**Current approach is recommended because:**
- Cache is optional - works without Redis
- Reduces auth service load for repeated requests
- Configurable TTL via ServerConfig

### Graceful Shutdown

**Q: Why 30 second shutdown timeout?**

| Aspect | Description |
|--------|-------------|
| Context | Server shutdown |
| Options considered | 5s, 15s, 30s, 60s |
| Rationale | Balance between fast shutdown and draining connections |

**Current approach is recommended because:**
- Allows in-flight requests to complete
- WebSocket connections can close cleanly
- Not so long as to delay deployments

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestNew` | Server creation |
| `TestHealthEndpoint` | Health check |
| `TestAuthMiddleware` | Authentication flow |
| `TestAuthMiddleware_CacheHit` | Cache usage |
| `TestRedisCache` | Cache operations |

**Running:**
```bash
make test ./internal/server
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/rs/zerolog` | Logging |
| `github.com/redis/go-redis/v9` | Redis client |
| `internal/ctx` | Context utilities |
| `internal/websocket` | WebSocket handler |

**File Locations:**
- [`internal/server/server.go`](../../internal/server/server.go) - HTTP server
- [`internal/server/middleware.go`](../../internal/server/middleware.go) - Auth middleware
- [`internal/server/auth.go`](../../internal/server/auth.go) - Auth client
- [`internal/server/redis_cache.go`](../../internal/server/redis_cache.go) - Redis cache

## Related

- [Auth Client](auth.md) - Detailed auth client documentation
- [WebSocket Handler](../websocket/README.md) - WebSocket handling
- [Config](../../config/README.md) - Server configuration
