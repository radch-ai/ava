# internal/config

Environment-based configuration loading.

## Description

The config package loads application configuration from environment variables. It provides type-safe access to configuration values with validation.

## Config Loading Flow

```mermaid
flowchart TB
    Load[Load] --> Addr["GATEWAY_ADDR - default: :8080"]
    Load --> Level["GATEWAY_LOG_LEVEL - default: info"]
    Load --> Origins["GATEWAY_ALLOWED_ORIGINS - comma separated"]
    Load --> Conns["GATEWAY_MAX_CONNS - default: 0 = unlimited"]
    Addr --> Return[Return &Config]
    Level --> Return
    Origins --> Return
    Conns --> Return
    Return --> Config["ServerAddr, LogLevel, AllowedOrigins, MaxConns"]
    Config --> Validate[cfg.Validate - Check addr not empty]
    Validate --> Result[Return nil/err]
```

## Types and Functions

| Name | Kind | Location | Description |
|------|------|----------|-------------|
| Config | struct | [config.go#L11](../internal/config/config.go#L11) | Configuration struct definition |
| Load | func | [config.go#L19](../internal/config/config.go#L19) | Reads from environment |
| Validate | func | [config.go#L63](../internal/config/config.go#L63) | Checks required fields |

## Configuration Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `ServerAddr` | string | `:8080` | HTTP server bind address |
| `LogLevel` | string | `info` | Logging level |
| `AllowedOrigins` | []string | empty | WebSocket allowed origins |
| `MaxConns` | int | `0` (unlimited) | Max concurrent connections |
