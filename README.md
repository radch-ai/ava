# Gateway

WebSocket gateway for Nevaria agent tool execution with LLM proxy and intelligent routing.

## Quick Start

```bash
make build  # Build binary to bin/nevaria-gateway
make run    # Start server on :8080
make test   # Run tests
```

## Configuration

Environment-based configuration.

| Variable | Default | Description |
|----------|---------|-------------|
| GATEWAY_ADDR | :8080 | Server bind address |
| GATEWAY_LOG_LEVEL | info | Log level (debug, info, warn, error) |
| GATEWAY_ALLOWED_ORIGINS | (empty) | CORS origins (empty = deny all) |
| GATEWAY_MAX_CONNS | 0 | Max connections (0 = unlimited) |
| OPENAI_API_KEY | (empty) | OpenAI API key |
| ANTHROPIC_API_KEY | (empty) | Anthropic API key |
| GOOGLE_API_KEY | (empty) | Google API key |
| DEFAULT_LLM_MODEL | gpt-4o | Default LLM model |
| SYSTEM_PROMPT | (empty) | Default system prompt |

### Production Example

```bash
export GATEWAY_ADDR=":8080"
export GATEWAY_LOG_LEVEL="info"
export GATEWAY_ALLOWED_ORIGINS="https://app.example.com"
export GATEWAY_MAX_CONNS="10000"
export OPENAI_API_KEY="sk-..."
export DEFAULT_LLM_MODEL="gpt-4o"
```

### Development Example

```bash
export GATEWAY_ADDR=":8080"
export GATEWAY_LOG_LEVEL="debug"
export GATEWAY_ALLOWED_ORIGINS="*"
export GATEWAY_MAX_CONNS="0"
export OPENAI_API_KEY="sk-..."
export DEFAULT_LLM_MODEL="gpt-4o-mini"
```

## Architecture

See [architecture/](architecture/) for complete system documentation.
