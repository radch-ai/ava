# Config

This document describes the configuration types, functions, and methods provided by the `internal/config` package.

## Contents

- [Config](#config)
- [Load](#load)
- [Config.Validate](#configvalidate)

## Config

`Config` holds the application configuration.

```go
type Config struct {
    ServerAddr     string
    LogLevel       string
    AllowedOrigins []string
    MaxConns       int
}
```

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `ServerAddr` | `string` | The address and port for the server to listen on (e.g., `:8080`, `0.0.0.0:8080`) |
| `LogLevel` | `string` | The logging level (e.g., `debug`, `info`, `warn`, `error`) |
| `AllowedOrigins` | `[]string` | List of allowed CORS origins. Empty list denies all origins. Use `["*"]` to allow all. |
| `MaxConns` | `int` | Maximum concurrent WebSocket connections. Zero means unlimited. |

### Example

```go
cfg := &config.Config{
    ServerAddr:     ":8080",
    LogLevel:       "info",
    AllowedOrigins: []string{"https://example.com"},
    MaxConns:       10000,
}
```

## Load

`Load` reads configuration from environment variables and returns a `Config`.

```go
func Load() (*Config, error)
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `GATEWAY_ADDR` | Server bind address | `:8080` |
| `GATEWAY_LOG_LEVEL` | Logging level | `info` |
| `GATEWAY_ALLOWED_ORIGINS` | Comma-separated CORS origins | (empty = deny all) |
| `GATEWAY_MAX_CONNS` | Maximum concurrent connections | `0` (unlimited) |

### Return Values

- `*Config` - The loaded configuration
- `error` - Non-nil if configuration is invalid

### Errors

Returns an error if `GATEWAY_MAX_CONNS` is:
- Not a valid integer (e.g., `"abc"`)
- A negative number (e.g., `"-1"`)

### Example

```go
cfg, err := config.Load()
if err != nil {
    log.Fatalf("Failed to load config: %v", err)
}

fmt.Printf("Server: %s\n", cfg.ServerAddr)
fmt.Printf("Log Level: %s\n", cfg.LogLevel)
fmt.Printf("Allowed Origins: %v\n", cfg.AllowedOrigins)
fmt.Printf("Max Connections: %d\n", cfg.MaxConns)
```

### Allowed Origins Parsing

The `GATEWAY_ALLOWED_ORIGINS` environment variable is parsed as a comma-separated list. Whitespace around each origin is automatically trimmed.

```bash
# Single origin
export GATEWAY_ALLOWED_ORIGINS="https://example.com"

# Multiple origins
export GATEWAY_ALLOWED_ORIGINS="https://example.com,https://app.example.com"

# Origins with extra whitespace (will be trimmed)
export GATEWAY_ALLOWED_ORIGINS="https://example.com , https://app.example.com  "

# Allow all origins (development only!)
export GATEWAY_ALLOWED_ORIGINS="*"
```

### Max Connections Validation

The `GATEWAY_MAX_CONNS` environment variable must be a non-negative integer.

```bash
# Valid values
export GATEWAY_MAX_CONNS="0"      # Unlimited (default)
export GATEWAY_MAX_CONNS="5000"   # 5,000 connections
export GATEWAY_MAX_CONNS="100000" # 100,000 connections

# Invalid values (will cause Load to return error)
export GATEWAY_MAX_CONNS="-1"     # Negative: error
export GATEWAY_MAX_CONNS="abc"    # Not an integer: error
```

## Config.Validate

`Validate` checks that the configuration is valid.

```go
func (c *Config) Validate() error
```

### Validation Rules

| Field | Rule |
|-------|------|
| `ServerAddr` | Cannot be empty string |

### Return Values

- `error` - Non-nil if validation fails

### Example

```go
cfg := &config.Config{
    ServerAddr: ":8080",
    LogLevel:   "info",
}

if err := cfg.Validate(); err != nil {
    log.Fatalf("Invalid configuration: %v", err)
}

// Example error: "server address cannot be empty"
```

## Complete Usage Example

```go
package main

import (
    "log"
    "github.com/nevaria/gateway/internal/config"
)

func main() {
    // Load configuration from environment
    cfg, err := config.Load()
    if err != nil {
        log.Fatalf("Failed to load configuration: %v", err)
    }

    // Validate configuration
    if err := cfg.Validate(); err != nil {
        log.Fatalf("Invalid configuration: %v", err)
    }

    // Use configuration
    log.Printf("Starting server on %s", cfg.ServerAddr)
    log.Printf("Log level: %s", cfg.LogLevel)
    log.Printf("Allowed origins: %v", cfg.AllowedOrigins)
    log.Printf("Max connections: %d", cfg.MaxConns)

    // ... rest of application
}
```

## See Also

- [Configuration README](README.md) - Overview and quick start guide
