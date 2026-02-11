# Configuration

The configuration package provides centralized application configuration through environment variables. It handles server settings, logging, CORS origins, and connection limits with secure defaults and validation.

## Overview

- **Environment-based Configuration**: All settings loaded from environment variables
- **Secure Defaults**: Safe default values for production use
- **Validation**: Built-in configuration validation with clear error messages
- **CORS Support**: Comma-separated origin configuration with automatic trimming
- **Connection Limiting**: Optional max connection limits for resource management

## Documentation

| Document | Description |
|----------|-------------|
| [config.md](config.md) | Config struct, Load function, Validate method |

## Quick Start

```go
import "github.com/nevaria/gateway/internal/config"

// Load configuration from environment
cfg, err := config.Load()
if err != nil {
    log.Fatal(err)
}

// Validate configuration
if err := cfg.Validate(); err != nil {
    log.Fatal(err)
}

// Use configuration
fmt.Printf("Starting server on %s\n", cfg.ServerAddr)
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `GATEWAY_ADDR` | Server bind address | `:8080` |
| `GATEWAY_LOG_LEVEL` | Logging level (debug, info, warn, error) | `info` |
| `GATEWAY_ALLOWED_ORIGINS` | Comma-separated CORS origins | (empty = deny all) |
| `GATEWAY_MAX_CONNS` | Maximum concurrent connections (0 = unlimited) | `0` |

## Usage Examples

### Basic Configuration

```go
cfg, err := config.Load()
if err != nil {
    log.Fatal(err)
}
```

### Setting Environment Variables

```bash
# Production
export GATEWAY_ADDR=":8080"
export GATEWAY_LOG_LEVEL="info"
export GATEWAY_ALLOWED_ORIGINS="https://example.com,https://app.example.com"
export GATEWAY_MAX_CONNS="10000"

# Development (allow all origins)
export GATEWAY_ALLOWED_ORIGINS="*"
```

### Configuration Validation

```go
cfg := &config.Config{
    ServerAddr:     ":8080",
    LogLevel:       "debug",
    AllowedOrigins: []string{"https://example.com"},
    MaxConns:       5000,
}

if err := cfg.Validate(); err != nil {
    log.Fatalf("Invalid configuration: %v", err)
}
```

## Security Considerations

### CORS Origins

By default, `AllowedOrigins` is empty, which denies all cross-origin requests. This is a secure default for production. You must explicitly configure allowed origins:

```bash
# Allow specific origins
export GATEWAY_ALLOWED_ORIGINS="https://example.com,https://app.example.com"

# Allow all origins (development only!)
export GATEWAY_ALLOWED_ORIGINS="*"
```

### Connection Limits

Setting `GATEWAY_MAX_CONNS` helps prevent resource exhaustion:

```bash
# Limit to 10,000 connections
export GATEWAY_MAX_CONNS="10000"

# Unlimited connections (default)
export GATEWAY_MAX_CONNS="0"
```

## Error Handling

The `Load` function returns errors for invalid configuration:

```go
cfg, err := config.Load()
if err != nil {
    // Example errors:
    // - "invalid GATEWAY_MAX_CONNS: \"-1\""
    // - "invalid GATEWAY_MAX_CONNS: \"abc\""
    log.Fatal(err)
}
```

## Related Documentation

- [WebSocket Handler](../websocket/README.md) - WebSocket connection management
- [Agent Framework](../agent/README.md) - Agent runtime configuration
