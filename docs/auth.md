# auth (internal/auth)

**⚠️ PLANNED FEATURES - NOT YET IMPLEMENTED**

This document describes the planned Zitadel JWT authentication system. The current implementation uses an external auth service client - see [`client.go`](../internal/auth/client.go) for the actual code.

**Status:** This is the planned Wave 2 implementation. See [`.claude/plans/auth-billing-migration.md`](../.claude/plans/auth-billing-migration.md) for progress.

---

# Zitadel JWT Validation (Planned)

Zitadel JWT validation and authentication middleware for the gateway. Validates incoming JWTs using Zitadel JWKS (JSON Web Key Sets) and extracts user identity for request authorization.

## Overview

### JWT Validation Flow

```mermaid
flowchart TB
    Start([Incoming Request]) --> GetToken["**extractToken()**<br/>Extract from Authorization header"]
    GetToken -->|missing| MissingToken["return ErrNoToken"]
    GetToken -->|found| ParseToken["**parseToken()**<br/>Parse JWT string"]

    ParseToken -->|invalid format| InvalidToken["return ErrInvalidToken"]
    ParseToken -->|valid| GetKey["**getKey()**<br/>Get key from JWKS cache"]

    subgraph SGGetKey [" "]
        GetKey --> CheckCache{"Cache hit?"}
        CheckCache -->|yes| ReturnKey["return cached key"]
        CheckCache -->|no| FetchJWKS["**fetchJWKS()**<br/>Fetch from Zitadel"]
        FetchJWKS -->|error| JWKSError["return ErrJWKSFetch"]
        FetchJWKS -->|success| UpdateCache["Update cache"]
        UpdateCache --> ReturnKey
    end

    ReturnKey --> Validate["**validateJWT()**<br/>Verify signature and claims"]

    subgraph SGValidate [" "]
        Validate --> CheckSig["Check signature<br/>using key"]
        CheckSig -->|invalid| SigError["return ErrInvalidSignature"]
        CheckSig -->|valid| CheckIssuer["Verify iss claim<br/>== ZITADEL_ISSUER_URL"]
        CheckIssuer -->|mismatch| IssuerError["return ErrInvalidIssuer"]
        CheckIssuer -->|match| CheckAudience["Verify aud claim<br/>contains ZITADEL_AUDIENCE"]
        CheckAudience -->|missing| AudienceError["return ErrInvalidAudience"]
        CheckAudience -->|match| CheckExpiry["Verify exp claim<br/>> now()"]
        CheckExpiry -->|expired| ExpiryError["return ErrTokenExpired"]
        CheckExpiry -->|valid| ExtractSubject["Extract user_id<br/>from sub claim"]
    end

    ExtractSubject --> Success(["return *SessionInfo"])
    MissingToken & InvalidToken & JWKSError & SigError & IssuerError & AudienceError & ExpiryError --> Fail(["return error"])

    click GetKey href "#jwks-fetching"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Success,Fail mainFn
    class GetKey,Validate,FetchJWKS,CheckSig,CheckIssuer,CheckAudience,CheckExpiry internalFn
    class MissingToken,InvalidToken,JWKSError,SigError,IssuerError,AudienceError,ExpiryError errorFlow
```

## Current Implementation

The gateway currently uses an external auth service for session validation. See [`internal/auth/client.go`](../internal/auth/client.go) for the actual implementation.

### Auth Client (Current)

| Function | Description |
|----------|-------------|
| `NewAuthClient(baseURL, secret, logger)` | Creates auth service client |
| `ValidateSession(ctx, sessionToken)` | Validates session with external auth service |

| Error | Description |
|-------|-------------|
| `ErrSessionInvalid` | Session token invalid |
| `ErrSessionExpired` | Session expired |
| `ErrAuthUnavailable` | Auth service unreachable |
| `ErrInvalidResponse` | Invalid response from auth service |
| `ErrContextCanceled` | Context was canceled |

## Environment Variables (Current)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AUTH_SERVICE_URL` | No | `http://localhost:8081` | Auth service URL |
| `AUTH_TIMEOUT` | No | `2s` | Auth request timeout |
| `AUTH_INTERNAL_SECRET` | Yes (prod) | - | Secret for auth service calls |

## Files to be Created (Planned)

| File | Purpose |
|------|---------|
| `internal/auth/jwks.go` | JWKS fetching and JWT validation |
| `internal/auth/middleware.go` | JWT auth middleware |
| `internal/auth/validator.go` | JWT validation logic |

## Files to be Removed (After Migration)

| File | Reason |
|------|--------|
| `internal/auth/client.go` | Replaced by JWKS validation |

## See Also

- [Implementation Plan](../.claude/plans/auth-billing-migration.md) - Full migration details
- [Billing Documentation](billing.md) - RevenueCat webhook integration (partially implemented)
- [Configuration](config.md) - Environment variables
