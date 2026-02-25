# Gateway

WebSocket gateway for Nevaria agents with authentication, LLM proxy, and distributed tool execution.

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
| `GATEWAY_ALLOWED_ORIGINS` | (empty) | Comma-separated CORS origins for WebSocket |
| `GATEWAY_MAX_CONNS` | `0` | Max concurrent WebSocket connections (0 = unlimited) |

### Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `AUTH_SERVICE_URL` | `http://localhost:8081` | Zetidal API URL for session validation |
| `AUTH_TIMEOUT` | `2s` | Auth request timeout |
| `INTERNAL_SECRET` | (required) | Gateway secret for /_internal/* endpoints (min 16 chars) |

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
| `GATEWAY_MODEL_CONFIG` | `config/models.yaml` | Model configuration path |

## Production Setup

```bash
export GATEWAY_ADDR="0.0.0.0:8080"
export GATEWAY_LOG_LEVEL="info"
export GATEWAY_ALLOWED_ORIGINS="https://app.example.com"
export GATEWAY_MAX_CONNS="10000"

export AUTH_SERVICE_URL="http://zetidal:8081"
export INTERNAL_SECRET="${RANDOM_SECRET_MIN_16_CHARS}"

export REDIS_ADDR="redis:6379"
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-..."
```

## Endpoints

| Path | Method | Auth | Description |
|------|--------|------|-------------|
| `/health` | GET, HEAD | None | Health check |
| `/ws` | GET | Session cookie | WebSocket connection |
| `/_internal/*` | * | Bearer token | Internal service endpoints |

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
