# Gateway

WebSocket gateway for Ava agents with authentication, LLM proxy, and distributed tool execution.

## Quick Start

```bash
make build  # Build binary to bin/gateway
make run     # Start server
make test    # Run tests
```

## Configuration

All configuration is via environment variables.

### Server

| Variable | Default | Description |
|----------|---------|-------------|
| `GATEWAY_ADDR` | `:8080` | Server bind address |
| `GATEWAY_LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `GATEWAY_MAX_CONNS` | `10000` | Max concurrent WebSocket connections |

### Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `ZETIDAL_URL` | `http://localhost:8081` | Zetidal API URL for session validation |

### Redis (Required)

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDR` | (required) | Redis server address |
| `REDIS_PASSWORD` | (empty) | Redis password |

### LLM Providers

| Variable | Default | Description |
|----------|---------|-------------|
| `OPENAI_API_KEY` | (empty) | OpenAI API key |
| `ANTHROPIC_API_KEY` | (empty) | Anthropic API key |
| `GOOGLE_API_KEY` | (empty) | Google AI API key |

## Production Setup

```bash
export GATEWAY_ADDR="0.0.0.0:8080"
export GATEWAY_LOG_LEVEL="info"
export GATEWAY_MAX_CONNS="10000"

export ZETIDAL_URL="http://zetidal:8081"

export REDIS_ADDR="redis:6379"
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-..."
```

## Endpoints

| Path | Method | Auth | Description |
|------|--------|------|-------------|
| `/health` | GET, HEAD | None | Health check |
| `/ws` | GET | Session cookie | WebSocket connection |

## Development

```bash
# Run with debug logging
make run

# Run specific test
go test ./tests/unit/...

# Run integration tests (requires services)
go test ./tests/integration/... -tags=integration
```

## Architecture

For complete system architecture, component diagrams, and API documentation, see [docs/](docs/).
