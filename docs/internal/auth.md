# Auth

Zetidal API client for session validation. Validates session tokens via HTTP calls to the zetidal API.

## Overview

```mermaid
flowchart TB
    Start("ValidateSession(ctx, token)") --> BuildURL["url.Parse(baseURL + /auth/session)"]
    BuildURL --> CreateReq["http.NewRequestWithContext()"]
    CreateReq --> AddCookie["req.AddCookie(session=token)"]
    AddCookie --> DoReq["http.Client.Do(req)"]
    DoReq -->|error| CheckErr{context error?}
    CheckErr -->|canceled| ErrCanceled["return ErrContextCanceled"]
    CheckErr -->|timeout| ErrUnavailable["return ErrAuthUnavailable"]
    CheckErr -->|other| ErrOther["return ErrAuthUnavailable"]
    DoReq -->|ok| CheckStatus{status code?}
    CheckStatus -->|200| DecodeResp["json.Decode(response)"]
    CheckStatus -->|401| ErrInvalid["return ErrSessionInvalid"]
    CheckStatus -->|503| ErrUnavailable2["return ErrAuthUnavailable"]
    CheckStatus -->|other| ErrUnexpected["return ErrAuthUnavailable"]
    DecodeResp --> CheckAuth{authenticated?}
    CheckAuth -->|no| ErrInvalid2["return ErrSessionInvalid"]
    CheckAuth -->|yes| ExtractCookie["extract Set-Cookie"]
    ExtractCookie --> Done("return SessionInfo")

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Done mainFn
    class ErrCanceled,ErrUnavailable,ErrOther,ErrInvalid,ErrUnavailable2,ErrUnexpected,ErrInvalid2 errorFlow
    class BuildURL,CreateReq,AddCookie,DoReq,DecodeResp,ExtractCookie internalFn
```

## Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `defaultAuthURL` | string | `http://localhost:8081` | Default zetidal API URL |
| `defaultTimeout` | Duration | `2s` | Default request timeout |
| `sessionCookieName` | string | `session` | Session cookie name |

## Structs

### Client

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Client` | `baseURL` | `string` | Zetidal API base URL |
| | `Client` | `*http.Client` | HTTP client (exported for testing) |
| | `logger` | `interface{}` | Logger (zerolog.Logger) |

### SessionInfo

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `SessionInfo` | `UserID` | `string` | Validated user ID |
| | `ExpiresAt` | `time.Time` | Session expiration time |
| | `SetCookie` | `*http.Cookie` | Cookie for sliding expiration |

## Error Handling

| Error Type | Handler | Behavior |
|------------|---------|----------|
| `ErrSessionInvalid` | 401 response | Session not valid |
| `ErrSessionExpired` | (unused) | Session has expired |
| `ErrAuthUnavailable` | Network/503 error | Zetidal API unreachable |
| `ErrInvalidResponse` | JSON decode error | Malformed response |
| `ErrContextCanceled` | Context canceled | Request cancelled |

## Design Notes

### Sliding Expiration

**Q: Why capture SetCookie for sliding expiration?**

| Aspect | Description |
|--------|-------------|
| Context | Session refresh from zetidal API |
| Options considered | Ignore, Pass through, Re-fetch |
| Rationale | Zetidal controls session lifetime |

**Current approach is recommended because:**
- Zetidal decides when to extend sessions
- Gateway just forwards the cookie
- No session state in gateway
- Consistent session management

## Test Coverage

### Unit Tests (`auth_test.go`)

| Test Suite | Coverage |
|------------|----------|
| `TestNewAuthClient` | Client creation with defaults |
| `TestValidateSession_Success` | Successful validation |
| `TestValidateSession_InvalidSession` | 401 response handling |
| `TestValidateSession_AuthUnavailable` | 503 response handling |
| `TestValidateSession_Timeout` | Request timeout |
| `TestValidateSession_ContextCanceled` | Context cancellation |

**Running:**
```bash
make test ./internal/auth
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| Standard library | HTTP client, JSON, context |

**File Location:** [`internal/auth/client.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/auth/client.go)

## Related

- [Server Middleware](../server/README.md) - Auth middleware usage
- [Config Auth](../../config/README.md) - Auth configuration
