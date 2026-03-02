# Server

HTTP server, middleware, and routing for the gateway.

## Variables

| Variable | Type | Description |
|----------|------|-------------|
| `ErrSessionInvalid` | `error` | Session invalid error |
| `ErrSessionExpired` | `error` | Session expired error |
| `ErrZetidalUnavailable` | `error` | Zetidal API unavailable error |
| `ErrInvalidResponse` | `error` | Invalid response error |
| `ErrContextCanceled` | `error` | Context canceled error |
| `ErrCacheUnavailable` | `error` | Cache unavailable error |

## Interfaces

### AuthCache

```go
type AuthCache interface {
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key string, value string, ttl time.Duration) error
    Delete(ctx context.Context, key string) error
}
```

## Structs

### AuthMiddleware

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AuthMiddleware` | `authClient` | `*auth.Client` | Auth service client |
| | `cache` | `AuthCache` | Session cache (can be nil) |
| | `cacheTTL` | `time.Duration` | Cache TTL |

### RedisCache

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RedisCache` | `client` | `*redis.Client` | Redis client |

### SessionResponse

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `SessionResponse` | `Authenticated` | `bool` | Whether session is valid |
| | `UserID` | `string` | User identifier |
| | `ExpiresAt` | `int64` | Session expiration timestamp |

### responseWrapper

Private struct that wraps ResponseWriter to capture Set-Cookie headers.

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `responseWrapper` | `ResponseWriter` | `http.ResponseWriter` | Underlying writer |
| | `wroteHeader` | `bool` | Track if header written |

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
| | `ZetidalURL` | `string` | Auth service URL |
| | `AuthCacheTTL` | `time.Duration` | Auth cache TTL |

## Simple Methods

| Method | Returns | Description |
|--------|---------|-------------|
| `HTTPServer()` | `*http.Server` | Returns underlying HTTP server (for testing) |
| `WSHandler()` | `*websocket.Handler` | Returns WebSocket handler (for testing) |

## NewAuthMiddleware

Creates a new authentication middleware with optional cache.

```mermaid
flowchart TB
    Start(["NewAuthMiddleware(authURL, cache, cacheTTL)"]) --> NewAuth["auth.NewAuthClient(authURL, nil) → authClient"]
    NewAuth --> CreateMW["&AuthMiddleware{authClient, cache, cacheTTL} → mw"]
    CreateMW --> Done(["return mw"])

    click NewAuth href "auth.md#NewAuthClient"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Done mainFn
    class NewAuth localPkgs
```

## Authenticate

Validates session cookie and adds user_id to context.

```mermaid
flowchart TB
    Start(["Authenticate(next)"]) --> GetCookie["r.Cookie(session) → cookie, err"]
    GetCookie --> CheckCookie{cookie?}
    CheckCookie -->|missing| WriteAuthErr(["writeAuthError(w)"])
    CheckCookie -->|found| SessionToken["sessionToken = cookie.Value"]

    SessionToken --> CheckCache{cache?}
    CheckCache -->|yes| CacheGet["cache.Get(ctx, session:token) → cachedUserID, err"]
    CacheGet --> CheckCached{cachedUserID?}
    CheckCached -->|yes| SetUserID["userID = cachedUserID"]
    CheckCached -->|no| Validate
    CheckCache -->|no| Validate

    Validate["authClient.ValidateSession(ctx, sessionToken) → sessionInfo, err"]
    Validate --> CheckError{err?}
    CheckError -->|SessionInvalid| WriteAuthErr2(["writeAuthError(w)"])
    CheckError -->|other| Write503(["http.Error(w, 503)"])
    CheckError -->|no| SetInfo["userID = sessionInfo.UserID"]
    SetInfo --> SetCookie["setCookie = sessionInfo.SetCookie"]
    SetCookie --> CacheSetCheck{cache?}
    CacheSetCheck -->|yes| CacheSet["cache.Set(ctx, session:token, userID, cacheTTL)"]
    CacheSetCheck -->|no| WrapResponse
    CacheSet --> WrapResponse

    WrapResponse --> CheckSetCookie{setCookie?}
    CheckSetCookie -->|yes| WrapWriter["wrapper = &responseWrapper{w}"]
    WrapWriter --> HTTPSetCookie["http.SetCookie(wrapper, setCookie)"]
    CheckSetCookie -->|no| UseWrapper["wrapper = w"]
    HTTPSetCookie --> UseWrapper

    UseWrapper --> WithUserID["internalctx.WithUserID(r.Context(), userID) → ctx"]
    WithUserID --> Next(["next.ServeHTTP(wrapper, r.WithContext(ctx))"])

    click Validate href "auth.md#ValidateSession"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Next mainFn
    class Validate localPkgs
    class WriteAuthErr,WriteAuthErr2,Write503 errorFlow
```

## NewRedisCache

Creates a new Redis-backed cache.

```mermaid
flowchart TB
    Start(["NewRedisCache(client)"]) --> CreateCache["&RedisCache{client} → cache"]
    CreateCache --> Done(["return cache"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Done mainFn
```

## RedisCache.Get

Retrieves a value from Redis.

```mermaid
flowchart TB
    Start(["Get(ctx, key)"]) --> ClientGet["c.client.Get(ctx, key).Result() → val, err"]
    ClientGet --> CheckError{err?}
    CheckError -->|redis.Nil| ReturnNotFound(["return '', fmt.Errorf('key not found')"])
    CheckError -->|other| ReturnErr(["return '', err"])
    CheckError -->|no| ReturnVal(["return val, nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnVal mainFn
    class ReturnNotFound,ReturnErr errorFlow
```

## RedisCache.Set

Stores a value in Redis with TTL.

```mermaid
flowchart TB
    Start(["Set(ctx, key, value, ttl)"]) --> ClientSet["c.client.Set(ctx, key, value, ttl).Err() → err"]
    ClientSet --> Return(["return err"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## RedisCache.Delete

Removes a value from Redis.

```mermaid
flowchart TB
    Start(["Delete(ctx, key)"]) --> ClientDel["c.client.Del(ctx, key).Err() → err"]
    ClientDel --> Return(["return err"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

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

    click WrapAuth "#Authenticate" "Go to Authenticate"

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

## Start

Starts the HTTP server and handles graceful shutdown.

```mermaid
flowchart TB
    Start(["Start()"]) --> LogInfo["logger.Info()"]
    LogInfo --> CreateErrCh["make(chan error, 1) → errCh"]
    CreateErrCh --> GoListen["go s.httpServer.ListenAndServe()"]
    GoListen --> CreateSigCh["make(chan os.Signal, 1) → sigChan"]
    CreateSigCh --> NotifySignal["signal.Notify(sigChan, SIGINT, SIGTERM)"]
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

    CreateCtx["context.WithTimeout(30s) → ctx, cancel"]
    CreateCtx --> DoShutdown["s.httpServer.Shutdown(ctx) → err"]
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
    CloseCh --> ShutdownServer["s.httpServer.Shutdown(ctx) → err"]
    ShutdownServer --> Return(["return err"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## Design Notes

### Response Wrapper for Hijack

**Q: Why wrap ResponseWriter for Set-Cookie?**

| Aspect | Description |
|--------|-------------|
| Context | Sliding session expiration |
| Options considered | Ignore cookie, wrap response, middleware chain |
| Rationale | Must forward Set-Cookie from auth API to browser |

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

### Optional Auth Middleware

**Q: Why allow nil authMW in New()?**

| Aspect | Description |
|--------|-------------|
| Context | Backward compatibility and testing |
| Options considered | Require auth, optional auth, separate constructors |
| Rationale | NewWithRedis for production, New for testing/flexibility |

**Current approach is recommended because:**
- Backward compatible with existing code
- Easy testing without auth
- NewWithRedis provides recommended production setup
- Clear separation of concerns

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
- [`internal/server/server.go`](../internal/server/server.go) - HTTP server
- [`internal/server/middleware.go`](../internal/server/middleware.go) - Auth middleware
- [`internal/server/redis_cache.go`](../internal/server/redis_cache.go) - Redis cache
- [`internal/server/response.go`](../internal/server/response.go) - Response utilities

## Related

- [Auth Client](auth.md) - Auth service client
- [WebSocket Handler](websocket.md) - WebSocket handling
- [Config](../config/README.md) - Server configuration
