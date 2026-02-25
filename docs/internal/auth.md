# Auth

Zetidal API client for session validation. Validates session tokens via HTTP calls to the zetidal API.

## Overview

```mermaid
flowchart TB
    Start([ValidateSession]) --> ParseURL["url.Parse(baseURL + /auth/session) → reqURL, err"]
    ParseURL -->|error| ParseErr(["return ErrZetidalUnavailable"])
    ParseURL -->|ok| NewReq["http.NewRequestWithContext(ctx, GET, reqURL) → req, err"]
    NewReq -->|error| ReqErr(["return ErrZetidalUnavailable"])
    NewReq -->|ok| AddCookie["req.AddCookie(session, sessionToken)"]
    AddCookie --> DoReq["c.Client.Do(req) → resp, err"]
    DoReq -->|error| CheckErr{err?}
    CheckErr -->|canceled| CanceledErr(["return ErrContextCanceled"])
    CheckErr -->|timeout| TimeoutErr(["return ErrZetidalUnavailable"])
    CheckErr -->|other| DoErr(["return ErrZetidalUnavailable"])
    DoReq -->|ok| CheckStatus{resp.StatusCode?}
    CheckStatus -->|200| DecodeBody["json.Decode(resp.Body, &result)"]
    CheckStatus -->|401| InvalidErr(["return ErrSessionInvalid"])
    CheckStatus -->|503| UnavailableErr(["return ErrZetidalUnavailable"])
    CheckStatus -->|other| UnexpectedErr(["return ErrZetidalUnavailable"])
    DecodeBody -->|error| DecodeErr(["return ErrInvalidResponse"])
    DecodeBody -->|ok| CheckAuth{result.Authenticated?}
    CheckAuth -->|no| NotAuthErr(["return ErrSessionInvalid"])
    CheckAuth -->|yes| LoopCookies["for resp.Cookies()"]
    LoopCookies --> CheckCookie{cookie.Name==session?}
    CheckCookie -->|yes| ExtractCookie["setCookie = cookie"]
    CheckCookie -->|no| Continue(["continue"])
    ExtractCookie --> BuildInfo["build SessionInfo"]
    BuildInfo --> Done(["return SessionInfo"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Done,Continue mainFn
    class ParseErr,ReqErr,CanceledErr,TimeoutErr,DoErr,UnavailableErr,UnexpectedErr,DecodeErr,NotAuthErr errorFlow
```

## Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `defaultZetidalURL` | string | `http://localhost:8081` | Default zetidal API URL |
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
| `TestValidateSession_ZetidalUnavailable` | 503 response handling |
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
- [Config](../../config/README.md) - Zetidal configuration
