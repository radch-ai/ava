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

## Functions

### NewAuthClient

```mermaid
flowchart TB
    Start(["NewAuthClient(baseURL, logger)"]) --> CheckBaseURL{baseURL?}
    CheckBaseURL -->|empty| SetDefault["baseURL = defaultZetidalURL"]
    CheckBaseURL -->|set| CreateClient
    SetDefault --> CreateClient

    subgraph SGCreateClient [" "]
        CreateClient["&Client{baseURL, &http.Client{}, logger} → c"]
        CreateClient --> Done(["return c"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Done mainFn
    class CreateClient internalFn
```

### ValidateSession

```mermaid
flowchart TB
    Start(["ValidateSession(ctx, sessionToken)"]) --> ParseURL["url.Parse(baseURL + /auth/session) → reqURL, err"]
    ParseURL --> CheckParseErr{err?}
    CheckParseErr -->|yes| ReturnParseErr(["return fmt.Errorf(ErrZetidalUnavailable)"])
    CheckParseErr -->|no| NewReq["http.NewRequestWithContext(ctx, GET, reqURL) → req, err"]

    subgraph SGNewReq [" "]
        NewReq --> CheckReqErr{err?}
        CheckReqErr -->|yes| ReturnReqErr(["return fmt.Errorf(ErrZetidalUnavailable)"])
        CheckReqErr -->|no| SetContentType["req.Header.Set(Content-Type, application/json)"]
        SetContentType --> AddCookie["req.AddCookie(&http.Cookie{Name: session, Value: sessionToken})"]
    end

    AddCookie --> DoReq["c.Client.Do(req) → resp, err"]
    DoReq --> CheckDoErr{err?}
    CheckDoErr -->|Canceled| ReturnCanceled(["return fmt.Errorf(ErrContextCanceled)"])
    CheckDoErr -->|Timeout| ReturnTimeout(["return fmt.Errorf(ErrZetidalUnavailable)"])
    CheckDoErr -->|other| ReturnOtherErr(["return fmt.Errorf(ErrZetidalUnavailable)"])
    CheckDoErr -->|no| SwitchStatus{resp.StatusCode?}

    subgraph SGStatusOK [" "]
        SwitchStatus -->|200| DecodeBody["json.NewDecoder(resp.Body).Decode(&result) → err"]
        DecodeBody --> CheckDecodeErr{err?}
        CheckDecodeErr -->|yes| ReturnDecodeErr(["return fmt.Errorf(ErrInvalidResponse)"])
        CheckDecodeErr -->|no| CheckAuth{result.Authenticated?}
        CheckAuth -->|no| ReturnNotAuth(["return ErrSessionInvalid"])
        CheckAuth -->|yes| LoopCookies["for resp.Cookies()"]
    end

    subgraph SGLoopCookies [" "]
        LoopCookies --> GetCookie["cookie := range"]
        GetCookie --> CheckCookieName{cookie.Name==session?}
        CheckCookieName -->|yes| SetSetCookie["setCookie = cookie"]
        CheckCookieName -->|no| Continue(["continue"])
        SetCookie --> BuildInfo
        Continue --> LoopCookies
    end

    subgraph SGBuildInfo [" "]
        LoopCookies -->|done| BuildInfo["&SessionInfo{UserID, ExpiresAt, SetCookie} → info"]
        BuildInfo --> CheckExpiresAt{result.ExpiresAt?}
        CheckExpiresAt -->|yes| SetExpiresAt["info.ExpiresAt = time.Unix(result.ExpiresAt, 0)"]
        CheckExpiresAt -->|no| CheckSetCookie
        SetExpiresAt --> CheckSetCookie
        CheckSetCookie -->|yes| InfoSetCookie["info.SetCookie = setCookie"]
        CheckSetCookie -->|no| ReturnOK
        InfoSetCookie --> ReturnOK(["return info, nil"])
    end

    SwitchStatus -->|401| ReturnInvalid(["return ErrSessionInvalid"])
    SwitchStatus -->|503| ReturnUnavailable(["return ErrZetidalUnavailable"])
    SwitchStatus -->|other| ReturnUnexpected(["return fmt.Errorf(ErrZetidalUnavailable)"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnOK,Continue mainFn
    class ParseURL,NewReq,AddCookie,DoReq,DecodeBody,LoopCookies,BuildInfo internalFn
    class ReturnParseErr,ReturnReqErr,ReturnCanceled,ReturnTimeout,ReturnOtherErr,ReturnDecodeErr,ReturnNotAuth,ReturnInvalid,ReturnUnavailable,ReturnUnexpected errorFlow
```

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
