# Config

Centralized environment-based configuration loading for the gateway application. Provides typed configuration structs with environment variable binding, validation, and logger initialization.

## Overview

### Load

```mermaid
flowchart TB
    Start([Load]) --> GetAddr["os.Getenv('GATEWAY_ADDR') → addr"]
    GetAddr --> GetLevel["os.Getenv('GATEWAY_LOG_LEVEL') → logLevel"]
    GetLevel --> GetOrigins["os.Getenv('GATEWAY_ALLOWED_ORIGINS') → value"]
    GetOrigins --> ParseOrigins["parseList(value) → origins"]
    ParseOrigins --> GetMaxConns["os.Getenv('GATEWAY_MAX_CONNS') → maxConnsStr"]
    GetMaxConns --> ParseMax["strconv.Atoi(maxConnsStr) → maxConns, err"]
    ParseMax -->|error| DoneErr("return error")
    ParseMax --> CheckNeg["maxConns < 0"]
    CheckNeg -->|true| DoneErr
    CheckNeg -->|false| GetZetidalURL["os.Getenv('ZETIDAL_URL') → zetidalURL"]
    GetZetidalURL --> GetRedis["os.Getenv('REDIS_ADDR') → redisAddr"]
    GetRedis --> GetRedisPwd["os.Getenv('REDIS_PASSWORD') → redisPassword"]
    GetRedisPwd --> GetModelCfg["os.Getenv('GATEWAY_MODEL_CONFIG') → modelConfigPath"]
    GetModelCfg --> DoneOk("return &Config{}, nil")

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,DoneOk mainFn
    class ParseMax,DoneErr errorFlow
```

### Validate

```mermaid
flowchart TB
    Start([Validate]) --> CheckAddr["check ServerAddr"]
    CheckAddr --> CheckRedis["check RedisAddr"]
    CheckRedis --> CheckURL["validate Zetidal URL"]
    CheckURL -->|ok| DoneOk("return nil")
    CheckURL -->|error| DoneErr("return error")

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,DoneOk mainFn
    class DoneErr errorFlow
```

## Structs

### Config

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Config` | `ServerAddr` | `string` | Server bind address |
| | `LogLevel` | `string` | Log level (debug, info, warn, error) |
| | `AllowedOrigins` | `[]string` | CORS allowed origins |
| | `MaxConns` | `int` | Max concurrent WebSocket connections |
| | `ZetidalURL` | `string` | Zetidal API URL for session validation |
| | `RedisAddr` | `string` | Redis server address |
| | `RedisPassword` | `string` | Redis password |
| | `ModelConfigPath` | `string` | Path to model configuration YAML |

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `GATEWAY_ADDR` | No | `:8080` | Server bind address |
| `GATEWAY_LOG_LEVEL` | No | `info` | Log level (debug, info, warn, error) |
| `GATEWAY_ALLOWED_ORIGINS` | No | - | Comma-separated CORS allowed origins |
| `GATEWAY_MAX_CONNS` | No | `0` (unlimited) | Max concurrent WebSocket connections |
| `GATEWAY_MODEL_CONFIG` | No | `config/models.yaml` | Path to model configuration YAML |
| `ZETIDAL_URL` | No | `http://localhost:8081` | Zetidal API URL for session validation |
| `REDIS_ADDR` | Yes | - | Redis server address (required) |
| `REDIS_PASSWORD` | No | - | Redis password |

## Design Notes

### AllowedOrigins Empty Default

**Q: Why is empty origins list "allow all" in development?**

| Aspect | Description |
|--------|-------------|
| Context | WebSocket and HTTP CORS configuration |
| Options considered | Allow all, Deny all, Require explicit |
| Rationale | Development convenience while warning operator |

**Current approach is recommended because:**
- Prevents accidental open CORS in production
- Clear warning message guides operators
- Development can use default without configuration
- Production requires explicit configuration

### Redis Required Configuration

**Q: Why is Redis required?**

| Aspect | Description |
|--------|-------------|
| Context | Redis for session caching, quota management, and rate limiting |
| Options considered | Required with HA, Optional, Not implemented |
| Rationale | Essential for production reliability and performance |

**Current approach is recommended because:**
- Session caching reduces API calls to zetidal
- Quota management requires persistent state
- Health checks and circuit breakers need Redis
- HA deployment ensures no data loss on failures

### Zetidal Session Validation

**Q: Why no configurable timeout for zetidal API?**

| Aspect | Description |
|--------|-------------|
| Context | Internal service-to-service communication |
| Options considered | Configurable timeout, Default timeout only |
| Rationale | Both services are internal, let zetidal control its own timeouts |

**Current approach is recommended because:**
- Gateway and zetidal are both internal services
- Zetidal manages its own request lifecycle
- Removes unnecessary configuration complexity
- Simpler operational model

## Test Coverage

### Unit Tests (`config_test.go`)

| Test Suite | Coverage |
|------------|----------|
| `TestLoadFromEnv` | Environment variable override |
| `TestDefaultValues` | Default configuration loading |
| `TestValidate` | Server address validation |
| `TestLoadAllowedOrigins` | CORS origins parsing |
| `TestLoadMaxConns` | Max connections validation |
| `TestValidateValidConfig` | Full config validation |
| `TestZetidalConfigDefaults` | Zetidal URL defaults |
| `TestZetidalConfigFromEnv` | Zetidal URL from env |

**Running:**
```bash
make test ./config
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/rs/zerolog` | Structured logging |

**File Locations:**
- [`config/config.go`](https://github.com/nevaria-ai/gateway/blob/main/config/config.go) - Main configuration

## Related

- [Models Configuration](models.md) - LLM model configuration
- [Plans Configuration](plans.md) - Subscription plan definitions
