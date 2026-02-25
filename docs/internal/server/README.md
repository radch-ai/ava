# Server

HTTP server, middleware, and routing for the gateway.

## Overview

```mermaid
flowchart TB
    Start([Request]) --> NewMux["http.NewServeMux() → mux"]
    NewMux --> HandleHealth["mux.HandleFunc(health)"]
    NewMux --> HandleWS["mux.Handle(ws, wsHandlerFunc)"]
    HandleWS --> CheckAuth{authMW?}
    CheckAuth -->|yes| AuthFunc["authMW.Authenticate(wsHandlerFunc) → http.Handler"]
    CheckAuth -->|no| UseHandler["wsHandlerFunc"]
    AuthFunc --> CheckCookie{session cookie?}
    CheckCookie -->|missing| AuthErr(["write 401"])
    CheckCookie -->|found| CheckCache{cache?}
    CheckCache -->|yes| GetCache["cache.Get(sessionToken) → userID"]
    GetCache -->|no userID| Validate["authClient.ValidateSession(sessionToken) → sessionInfo"]
    GetCache -->|userID set| WithUserID["ctx.WithUserID(userID) → ctx"]
    CheckCache -->|no| Validate
    Validate -->|error| ServiceErr(["write 503"])
    Validate -->|ok| WithUserID
    WithUserID --> SetCookie{setCookie?}
    SetCookie -->|yes| SetWrap["http.SetCookie(wrapper, setCookie)"]
    SetCookie -->|no| Next["next.ServeHTTP(wrapper, r.WithContext(ctx))"]
    SetWrap --> Next
    UseHandler --> Next
    Next --> Done([Handler Called])

    click Validate href "auth.md"
    click GetCache href "auth.md"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Done mainFn
    class Validate,GetCache localPkgs
    class AuthErr,ServiceErr errorFlow
```

## Structs

### Server

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Server` | `httpServer` | `*http.Server` | HTTP server |
| | `logger` | `zerolog.Logger` | Logger |
| | `wsHandler` | `*websocket.Handler` | WebSocket handler |
| | `authMW` | `*AuthMiddleware` | Auth middleware |
| | `shutdownCh` | `chan struct{}` | Shutdown signal |

### AuthMiddleware

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `AuthMiddleware` | `authClient` | `*auth.Client` | Auth service client |
| | `cache` | `Cache` | Session cache |
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

### New

```mermaid
flowchart TB
    Start(["New(addr, logger, wsHandler, authMW)"]) --> NewMux["http.NewServeMux() → mux"]

    subgraph SGHandleHealth [" "]
        NewMux --> HandleHealth["mux.HandleFunc(health, healthHandler)"]
    end

    subgraph SGHandleWS [" "]
        NewMux --> HandleWS["mux.Handle(ws, wsHandlerFunc)"]
    end

    HandleHealth --> CheckMW{authMW?}
    CheckMW -->|yes| WrapAuth["authMW.Authenticate(wsHandlerFunc)"]
    CheckMW -->|no| UseHandler["wsHandlerFunc"]
    WrapAuth --> WrapWS["mux.Handle(ws, wrapped)"]
    UseHandler --> WrapWS
    WrapWS --> CreateServer

    subgraph SGCreateServer [" "]
        WrapWS --> CreateServer["&Server{httpServer, logger, wsHandler, authMW, shutdownCh} → srv"]
        CreateServer --> Done(["return srv"])
    end

    click WrapAuth href "#Authenticate"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Done mainFn
    class WrapAuth localPkgs
    class HandleHealth,HandleWS,WrapWS,CreateServer internalFn
```

### Start

```mermaid
flowchart TB
    Start(["Start()"]) --> LogInfo["logger.Info()"]
    LogInfo --> CreateErrCh["make(chan error, 1) → errCh"]

    subgraph SGListen [" "]
        CreateErrCh --> GoListen["go s.httpServer.ListenAndServe()"]
        GoListen --> CheckErr{err?}
        CheckErr -->|yes| SendErr["errCh ← err"]
        CheckErr -->|no| ReturnListen
        ReturnListen(["return"])
    end

    CreateErrCh --> CreateSigCh["make(chan os.Signal, 1) → sigChan"]
    CreateSigCh --> NotifySignal["signal.Notify(sigChan, SIGINT, SIGTERM)"]

    subgraph SGSelect [" "]
        NotifySignal --> Select["select"]
        Select -->|errCh| ErrCh["err := <-errCh"]
        ErrCh --> LogError["logger.Error()"]
        LogError --> ReturnErr(["return err"])
        Select -->|sigChan| Sig["sig := <-sigChan"]
        Sig --> LogSignal["logger.Info()"]
        LogSignal --> CreateCtx["context.WithTimeout(30s) → ctx, cancel"]
        Select -->|shutdownCh| ShutdownCh["<-s.shutdownCh"]
        ShutdownCh --> LogShutdown["logger.Info()"]
        LogShutdown --> CreateCtx
    end

    subgraph SGShutdown [" "]
        CreateCtx --> Shutdown["s.httpServer.Shutdown(ctx) → err"]
        Shutdown --> CheckShutdownErr{err?}
        CheckShutdownErr -->|yes| LogShutdownErr["logger.Error()"]
        LogShutdownErr --> ReturnShutdownErr(["return fmt.Errorf()"])
        CheckShutdownErr -->|no| LogStopped["logger.Info()"]
        LogStopped --> ReturnNil(["return nil"])
    end

    click Shutdown href "#Shutdown"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnListen,ReturnNil mainFn
    class Shutdown localPkgs
    class ReturnErr,ReturnShutdownErr errorFlow
```

### Shutdown

```mermaid
flowchart TB
    Start(["Shutdown(ctx)"]) --> CloseCh["close(s.shutdownCh)"]
    CloseCh --> ShutdownServer["s.httpServer.Shutdown(ctx) → err"]
    ShutdownServer --> Return(["return err"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### HTTPServer

```mermaid
flowchart TB
    Start(["HTTPServer()"]) --> Return(["return s.httpServer"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### WSHandler

```mermaid
flowchart TB
    Start(["WSHandler()"]) --> Return(["return s.wsHandler"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### NewAuthMiddleware

```mermaid
flowchart TB
    Start(["NewAuthMiddleware(authURL, cache, cacheTTL)"]) --> NewAuth["auth.NewAuthClient(authURL, nil) → authClient"]

    subgraph SGCreateMW [" "]
        NewAuth --> CreateMW["&AuthMiddleware{authClient, cache, cacheTTL} → mw"]
        CreateMW --> Done(["return mw"])
    end

    click NewAuth href "../auth.md#NewAuthClient"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Done mainFn
    class NewAuth localPkgs
    class CreateMW internalFn
```

### Authenticate

```mermaid
flowchart TB
    Start(["Authenticate(next)"]) --> GetCookie["r.Cookie(session) → cookie, err"]
    GetCookie --> CheckCookie{cookie?}
    CheckCookie -->|missing| WriteAuthErr(["writeAuthError(w)"])
    CheckCookie -->|found| SessionToken["sessionToken = cookie.Value"]

    subgraph SGCheckCache [" "]
        SessionToken --> CheckCache{cache?}
        CheckCache -->|yes| CacheGet["cache.Get(ctx, session:token) → cachedUserID, err"]
        CacheGet --> CheckCached{cachedUserID?}
        CheckCached -->|yes| UserID["userID = cachedUserID"]
        CheckCached -->|no| Validate
        CheckCache -->|no| Validate
    end

    subgraph SGValidate [" "]
        Validate --> ValidateSession["authClient.ValidateSession(ctx, sessionToken) → sessionInfo, err"]
        ValidateSession --> CheckError{err?}
        CheckError -->|SessionInvalid| WriteAuthErr2(["writeAuthError(w)"])
        CheckError -->|other| Write503(["http.Error(w, 503)"])
        CheckError -->|no| SetUserID["userID = sessionInfo.UserID"]
        SetUserID --> SetCookie["setCookie = sessionInfo.SetCookie"]
        SetCookie --> CacheSet
    end

    subgraph SGCacheSet [" "]
        SetCookie --> CheckCacheSet{cache?}
        CheckCacheSet -->|yes| CacheSet["cache.Set(ctx, session:token, userID, cacheTTL)"]
        CheckCacheSet -->|no| WrapResponse
        CacheSet --> WrapResponse
    end

    subgraph SGWrapResponse [" "]
        WrapResponse --> CheckSetCookie{setCookie?}
        CheckSetCookie -->|yes| WrapWriter["wrapper = &responseWrapper{w}"]
        WrapWriter --> HTTPSetCookie["http.SetCookie(wrapper, setCookie)"]
        CheckSetCookie -->|no| UseWrapper["wrapper = w"]
        HTTPSetCookie --> UseWrapper
    end

    UseWrapper --> WithUserID["internalctx.WithUserID(r.Context(), userID) → ctx"]
    WithUserID --> Next["next.ServeHTTP(wrapper, r.WithContext(ctx))"]

    click ValidateSession href "../auth.md#ValidateSession"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Next,WriteAuthErr,WriteAuthErr2,Write503 mainFn
    class ValidateSession localPkgs
    class CacheGet,CacheSet,WrapWriter errorFlow
```

### NewRedisCache

```mermaid
flowchart TB
    Start(["NewRedisCache(client)"]) --> CreateCache["&RedisCache{client} → cache"]
    CreateCache --> Done(["return cache"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Done mainFn
```

### Cache.Get

```mermaid
flowchart TB
    Start(["Get(ctx, key)"]) --> ClientGet["c.client.Get(ctx, key) → val, err"]
    ClientGet --> CheckError{err?}
    CheckError -->|Nil| ReturnNotFound(["return '', fmt.Errorf()"])
    CheckError -->|other| ReturnErr(["return '', err"])
    CheckError -->|no| ReturnVal(["return val, nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnVal mainFn
    class ReturnNotFound,ReturnErr errorFlow
```

### Cache.Set

```mermaid
flowchart TB
    Start(["Set(ctx, key, value, ttl)"]) --> ClientSet["c.client.Set(ctx, key, value, ttl).Err() → err"]
    ClientSet --> Return(["return err"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### Cache.Delete

```mermaid
flowchart TB
    Start(["Delete(ctx, key)"]) --> ClientDel["c.client.Del(ctx, key).Err() → err"]
    ClientDel --> Return(["return err"])

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
| Rationale | Must forward Set-Cookie from zetidal API to browser |

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
