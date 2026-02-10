# Gateway Foundation

## Overview

The Nevaria Gateway is a multi-tenant WebSocket gateway for AI agent tool routing.

## Project Structure

### Project Structure
```
gateway/
├── cmd/gateway/main.go          # Application entry point
├── internal/
│   ├── server/                  # HTTP server with graceful shutdown
│   ├── config/                  # Environment-based configuration
│   ├── websocket/               # WebSocket handler with connection tracking
│   └── project/                 # Dynamic project naming from parent
├── scripts/                     # Code generation scripts
├── docs/                        # Project documentation
├── .claude/                     # Claude-specific (plans, brainstorming)
├── Makefile                     # Build automation
├── go.mod                       # Go module definition
└── README.md                    # Project overview
```

### Components

#### Server (`internal/server/`)
- HTTP server with configurable address
- Graceful shutdown on SIGINT/SIGTERM
- Integrated logging with project context
- Health check endpoint handler

#### Configuration (`internal/config/`)
- Environment-based config:
  - `GATEWAY_ADDR`: Server address (default: `:8080`)
  - `GATEWAY_LOG_LEVEL`: Log level (default: `info`)
  - `GATEWAY_ALLOWED_ORIGINS`: Comma-separated origin whitelist (default: deny all)
  - `GATEWAY_MAX_CONNS`: Maximum concurrent connections (default: 0 = unlimited)

#### WebSocket Handler (`internal/websocket/`)
- WebSocket upgrade using gorilla/websocket
- Unique connection ID (UUID) per connection
- Single-writer pattern for concurrent safety
- Connection limiting with semaphore
- Origin validation via whitelist
- Ping/pong keep-alive (30s interval)
- sync.Pool for message memory optimization
- Graceful connection closure

#### Project Details (`internal/project/`)
- Generated from `../project-details/project-name-template.json`
- Provides: Name, Slug, Description, ModulePath, BinaryName
- Regenerate with: `go run scripts/generate-project-details.go`

### Technology Stack

| Component | Choice | Purpose |
|-----------|--------|---------|
| Language | Go 1.21+ | Performance, concurrency |
| WebSocket | gorilla/websocket | Standard Go WebSocket lib |
| Logging | zerolog | Structured, zero-allocation |
| UUID | google/uuid | Connection IDs |
| Testing | testify | Assertions, test suite |

### Running

```bash
# Build
make build

# Run
make run

# Test
make test

# Regenerate project details
make generate
```

### Architecture

- **Deployment:** Single shared multi-tenant gateway
- **Protocol:** WebSocket (OpenClaw compatible)
- **Configuration:** Environment variables (12-factor app)
- **Logging:** Structured JSON with zerolog
- **Module path:** `github.com/nevaria/gateway`
