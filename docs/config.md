# config

Centralized environment-based configuration loading for the gateway application. Provides typed configuration structs with environment variable binding, validation, and logger initialization.

## Overview

### Load Function

Reads configuration from environment variables and returns a `*Config` struct with all typed fields (Server, Auth, Redis).

### Structs and Environment Variables

| Config Struct | Field | Env Variable | Type | Default | Required | Description |
|---------------|-------|--------------|------|---------|----------|-------------|
| **Server** | `Addr` | `GATEWAY_ADDR` | `string` | `:8080` | No | Server bind address |
| | `LogLevel` | `GATEWAY_LOG_LEVEL` | `string` | `info` | No | Log level (debug, info, warn, error) |
| | `AllowedOrigins` | `GATEWAY_ALLOWED_ORIGINS` | `[]string` | - | No | Comma-separated CORS allowed origins |
| | `MaxConns` | `GATEWAY_MAX_CONNS` | `int` | `0` | No | Max concurrent WebSocket connections (0 = unlimited) |
| **Auth** | `URL` | `AUTH_SERVICE_URL` | `string` | `http://localhost:8081` | No | Auth service URL |
| | `Timeout` | `AUTH_TIMEOUT` | `time.Duration` | `2s` | No | Auth request timeout |
| | `AuthInternalSecret` | `AUTH_INTERNAL_SECRET` | `string` | - | Prod* | Secret for auth service calls |
| | `InternalSecret` | `INTERNAL_SECRET` | `string` | - | Prod* | Gateway internal secret (/_internal/*) |
| **Redis** | `Addr` | `REDIS_ADDR` | `string` | - | No | Redis server address |
| | `Password` | `REDIS_PASSWORD` | `string` | - | No | Redis password |
| **Mode** | - | `ENV` | `string` | - | No | Production mode flag (set to `production`) |
| | - | `APP_ENV` | `string` | - | No | Alternative production mode flag (set to `prod`) |

\* Required only when `ENV=production` or `APP_ENV=prod`

### AuthConfig

```go
type AuthConfig struct {
    URL                string        // Auth service base URL
    Timeout            time.Duration // Auth request timeout
    AuthInternalSecret string        // Secret for calling auth service
    InternalSecret     string        // Gateway's internal secret for /_internal/*
}
```

### Error Handling

| Error Type | Handler | Exit Code / Behavior |
|------------|---------|---------------------|
| Production without secrets | Returns error from `Validate()` | Caller handles (typically fatal) |
| Invalid `GATEWAY_ADDR` | Returns error from `Validate()` | Caller handles (typically fatal) |
| Invalid `AUTH_SERVICE_URL` | Returns error from `Validate()` | Caller handles (typically fatal) |
| Invalid `AUTH_TIMEOUT` | Returns error from `Load()` | Caller handles (typically fatal) |

### Validation

The `Config.Validate()` method performs the following checks:

| Check | Condition | Error Message |
|-------|-----------|---------------|
| Server address | `ServerAddr` is empty | `server address cannot be empty` |
| Production auth secret | `ENV=production` and `AuthInternalSecret` is empty | `AUTH_INTERNAL_SECRET required in production` |
| Auth secret length | `AuthInternalSecret` < 16 characters | `AUTH_INTERNAL_SECRET must be at least 16 characters` |
| Production internal secret | `ENV=production` and `InternalSecret` is empty | `INTERNAL_SECRET required in production` |
| Internal secret length | `InternalSecret` < 16 characters | `INTERNAL_SECRET must be at least 16 characters` |
| Auth URL format | `URL` is not a valid HTTP(S) URL | `AUTH_SERVICE_URL must be a valid URL with http or https scheme` |

### Log Levels

| Level | Description | Usage |
|-------|-------------|-------|
| `debug` | Detailed debugging information | Development/troubleshooting |
| `info` | General informational messages | Default production level |
| `warn` | Warning messages | Potential issues |
| `error` | Error messages only | Production errors |

### Logger Behavior

| Environment | Output Format |
|-------------|---------------|
| Production (`ENV=production`) | JSON (structured logs) |
| Development | Console (pretty-printed with timestamps) |

## Design Notes

### Auth Service Timeout

**Q: Why is the default auth timeout 2 seconds?**

**A:** The auth service validation is on the critical path for every WebSocket connection. It needs to be fast but should allow for network latency.

| Aspect | Description |
|--------|-------------|
| Context | Auth service session validation |
| Options considered | 500ms, 1s, 2s, 5s |
| Rationale | 2s allows for network delays while failing fast on actual issues |

**Current approach is recommended because:**
- Fast enough for good UX
- Allows for network latency
- Fails fast on actual auth service issues
- Configurable via environment variable

### AllowedOrigins Empty Default

**Q: Why is empty origins list "deny all"?**

**A:** Security by default. An explicit list of allowed origins prevents CORS misconfiguration.

| Aspect | Description |
|--------|-------------|
| Context | WebSocket and HTTP CORS configuration |
| Options considered | Allow all, Deny all, Require explicit |
| Rationale | Requiring explicit configuration prevents security issues |

**Current approach is recommended because:**
- Prevents accidental open CORS
- Clear error message guides operators
- Development can use explicit `*` if needed
- Standard security pattern

### Redis Optional Configuration

**Q: Why is Redis configuration optional?**

**A:** Redis is currently used for optional features like session caching. The gateway can operate without it.

| Aspect | Description |
|--------|-------------|
| Context | Redis for caching and rate limiting |
| Options considered | Required, Optional, Not implemented |
| Rationale | Allows operation without external dependencies |

**Current approach is recommended because:**
- Gateway can function without Redis
- Features that use Redis can degrade gracefully
- Development doesn't require Redis setup
- Production can enable Redis for performance

### Internal Secret Requirements

**Q: Why 16 character minimum for secrets?**

**A:** Provides sufficient entropy for security against brute force attacks.

| Aspect | Description |
|--------|-------------|
| Context | Secret-based authentication |
| Options considered | 8, 16, 32 characters |
| Rationale | 16 characters provides good security/usability balance |

**Current approach is recommended because:**
- Sufficient entropy for HMAC/signing
- Not so long as to be unusable
- Standard security practice
- Easy to generate with password managers

## Test Coverage

### Unit Tests (`config_test.go`)

White box tests that validate configuration loading and validation.

| Test Suite | Coverage |
|------------|----------|
| `TestLoad` | Default configuration loading |
| `TestLoad_FromEnv` | Environment variable override |
| `TestLoad_InvalidTimeout` | Invalid timeout duration handling |
| `TestValidate_ServerAddress` | Server address validation |
| `TestValidate_ProductionSecrets` | Production secret requirements |
| `TestValidate_SecretLength` | Secret length validation |
| `TestValidate_AuthURL` | Auth URL format validation |
| `TestNewLogger` | Logger creation for different environments |
| `TestParseLogLevel` | Log level parsing |
| `TestIsProduction` | Production detection |

**Running:**
```bash
make test ./config
```

### Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/rs/zerolog` | Structured logging |

**File Locations:**
- [`config/config.go`](../config/config.go) - Main configuration
- [`config/auth.go`](../config/auth.go) - Auth configuration
- [`config/config_test.go`](../config/config_test.go) - Tests

## See Also

- [Auth Documentation](auth.md) - Auth service client (current implementation)
- [Implementation Plan](../.claude/plans/auth-billing-migration.md) - Planned Zitadel/RevenueCat migration
