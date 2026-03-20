# Claude Code Context - backend

## Tech Stack

- **Language**: Go
- **Database**: PostgreSQL
- **API Style**: REST

## Project Commands

**IMPORTANT:** **Never skip make commands** - always use `make` targets instead of direct `go` commands. Read Makefile only if you need more info, never run non-make alternatives.

Available targets:
- `make deps` - Install dependencies (instead of `go get`/`go mod tidy`)
- `make install-tools` - Install dev tools (instead of direct tool installation)
- `make build` - Build binary (instead of `go build`)
- `make run` - Run application
- `make test` / `make test-unit` / `make test-integration` / `make test-e2e` - Testing (instead of `go test`)
- `make lint` / `make fmt` - Code quality
- `make generate` - Run code generation (sqlc, etc.)
- `make help` - Show all available targets

## Project Documentation

**IMPORTANT:** Read `docs/README.md` first to understand project overview, development conventions and structure.

## Architecture Notes

- REST API backend for user management and billing
- RevenueCat webhook handling for subscription management
- User configuration management (soul, agents, etc.)
- PostgreSQL database with migrations
- Serves as the central backend for:
  - Billing and subscription management
  - User configuration (soul.md, agents.md, etc.)
  - Agent settings persistence
  - Usage tracking and analytics

## Service Relationships

- **gateway**: WebSocket gateway for real-time agent communication. Calls backend for user config loading.
- **llm-proxy**: LLM quota and model routing service. Backend is separate from this.

## Key Packages

- `internal/billing` - RevenueCat webhook handling and subscription management
- `internal/user` - User configuration management (planned)
- `internal/agent` - Agent settings persistence (planned)
