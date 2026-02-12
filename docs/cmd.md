# cmd/gateway

Application entry point and service initialization.

## Description

The main package initializes all components and starts the HTTP server. It loads configuration from environment variables, initializes the model registry, sets up the WebSocket handler, and manages graceful shutdown.

## Initialization Flow

```mermaid
flowchart TB
    Start[main] --> Load[config.Load]
    Load --> Env1["GATEWAY_ADDR"]
    Load --> Env2["LOG_LEVEL"]
    Load --> Env3["ALLOWED_ORIGINS"]
    Load --> Env4["MAX_CONNS"]
    Env1 --> Validate[cfg.Validate]
    Env2 --> Validate
    Env3 --> Validate
    Env4 --> Validate
    Validate --> Logger[Configure zerolog logger]
    Logger --> Models[models.Get - Get singleton registry]
    Models --> LoadYAML[LoadFromYAML - Model config]
    LoadYAML --> Origins[Set default allowed origins - warn if empty]
    Origins --> MaxConns[Set default maxConns - 10000 if 0]
    MaxConns --> Handler[NewHandler - Create WS handler]
    Handler --> Server[server.New - Create HTTP server]
    Server --> SrvStart[srv.Start - Block until shutdown signal]
    SrvStart --> Exit[EXIT]
```

## Functions

| Name | Kind | Location | Description |
|------|------|----------|-------------|
| main | func | [main.go#L14](../cmd/gateway/main.go#L14) | Complete initialization flow |

## Configuration

| Environment Variable | Default | Description |
|----------------------|---------|-------------|
| `GATEWAY_ADDR` | `:8080` | Server bind address |
| `GATEWAY_LOG_LEVEL` | `info` | Log level (trace, debug, info, warn, error) |
| `GATEWAY_ALLOWED_ORIGINS` | empty | Comma-separated list of allowed WebSocket origins |
| `GATEWAY_MAX_CONNS` | `10000` | Maximum concurrent WebSocket connections |
| `GATEWAY_MODEL_CONFIG` | `config/models.yaml` | Path to model configuration file |
