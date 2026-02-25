# Server

HTTP server, middleware, and routing for the gateway.

## Overview

```mermaid
flowchart TB
    Start("Request") --> Mux["http.ServeMux"]
    Mux -->|health| Health["HealthHandler()"]
    Mux -->|ws| AuthMW["AuthMiddleware.Authenticate()"]
    Mux -->|internal| Internal["InternalRouter"]
    
    AuthMW -->|valid| WSHandler["WebSocket Handler"]
    AuthMW -->|invalid| AuthErr("401 Unauthorized")
    
    Internal -->|valid secret| InternalHandler["Internal handlers"]
    Internal -->|invalid secret| InternalErr("401 Unauthorized")

    click WSHandler href "../websocket.md"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start mainFn
    class Mux,Health,AuthMW,Internal,InternalHandler internalFn
    class WSHandler localPkgs
    class AuthErr,InternalErr errorFlow
```

## Structs

### Server

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Server` | `httpServer` | `*http.Server` | HTTP server |
| | `logger` | `zerolog.Logger` | Logger |
| | `wsHandler` | `*websocket.Handler` | WebSocket handler |
| | `authMW` | `*AuthMiddleware` | Auth middleware |
| | `internalRouter` | `http.Handler` | Internal endpoints |
| | `shutdownCh` | `chan struct{}` | Shutdown signal |

### AuthMiddleware

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AuthMiddleware` | `authClient` | `*auth.Client` | Auth service client |
| | `cache` | `Cache` | Session cache (optional) |
| | `cacheTTL` | `time.Duration` | Cache TTL |

### RedisCache

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RedisCache` | `client` | `*redis.Client` | Redis client |

## Interfaces

### Cache

```go
type Cache interface {
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key string, value string, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
}
```

## Functions

### Server

| Function | Signature | Description |
|----------|-----------|-------------|
| `New` | `(addr string, logger zerolog.Logger, wsHandler *websocket.Handler, authMW *AuthMiddleware, internalRouter http.Handler) *Server` | Creates server |
| `Start` | `() error` | Starts server |
| `Shutdown` | `(ctx context.Context) error` | Graceful shutdown |
| `HTTPServer` | `() *http.Server` | Returns HTTP server |
| `WSHandler` | `() *websocket.Handler` | Returns WebSocket handler |

### Middleware

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewAuthMiddleware` | `(authURL, secret string, cache Cache, cacheTTL time.Duration) *AuthMiddleware` | Creates auth middleware |
| `Authenticate` | `(next http.Handler) http.Handler` | Auth middleware handler |

### Internal

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewInternalRouter` | `(secret string, cache Cache) *mux.Router` | Creates internal router |
| `NewInternalMiddleware` | `(secret string) *InternalMiddleware` | Creates internal auth |
| `RequireInternalAuth` | `(next http.Handler) http.Handler` | Bearer token auth |

### Cache

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewRedisCache` | `(client *redis.Client) *RedisCache` | Creates Redis cache |
| `Get` | `(ctx context.Context, key string) (string, error)` | Gets value |
| `Set` | `(ctx context.Context, key string, value string, ttl time.Duration) error` | Sets value |
| `Delete` | `(ctx context.Context, key string) error` | Deletes value |

## Error Handling

| Error Type | Handler | Behavior |
|------------|---------|----------|
| Missing session cookie | 401 | `authentication_required` |
| Invalid session | 401 | `authentication_required` |
| Auth unavailable | 503 | Service unavailable |
| Invalid internal secret | 401/403 | Unauthorized/Forbidden |

## Design Notes

### Response Wrapper for Hijack

**Q: Why wrap ResponseWriter for Set-Cookie?**

| Aspect | Description |
|--------|-------------|
| Context | Sliding session expiration |
| Options considered | Ignore cookie, wrap response, middleware chain |
| Rationale | Must forward Set-Cookie from auth service to browser |

**Current approach is recommended because:**
- Preserves WebSocket upgrade capability (Hijacker interface)
- Forwards sliding expiration cookies
- Transparent to downstream handlers
- Standard middleware pattern

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
- Configurable via context in Shutdown()

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestNew` | Server creation |
| `TestHealthEndpoint` | Health check |
| `TestAuthMiddleware` | Authentication flow |
| `TestAuthMiddleware_CacheHit` | Cache usage |
| `TestInternalRouter` | Internal endpoints |
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
| `github.com/gorilla/mux` | Internal router |
| `internal/auth` | Auth client |
| `internal/ctx` | Context utilities |
| `internal/websocket` | WebSocket handler |

**File Locations:**
- [`internal/server/server.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/server/server.go) - HTTP server
- [`internal/server/middleware.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/server/middleware.go) - Auth middleware
- [`internal/server/internal.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/server/internal.go) - Internal router
- [`internal/server/redis_cache.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/server/redis_cache.go) - Redis cache
- [`internal/server/response.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/server/response.go) - Response utilities

## Related

- [Auth Client](auth.md) - Auth service client
- [WebSocket Handler](websocket.md) - WebSocket handling
- [Config](../../config/README.md) - Server configuration
