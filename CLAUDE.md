# Claude Code Context - backend

## Tech Stack

- **Language**: Go
- **Database**: PostgreSQL
- **API Style**: REST

## Project Commands

**IMPORTANT:** Always read the Makefile first to discover available commands. Run project-related operations via `make` targets rather than direct commands.

Key targets include:
- `make build` - Build binary
- `make run` - Run application
- `make test` / `make test-unit` / `make test-integration` / `make test-e2e` - Testing
- `make lint` / `make fmt` - Code quality
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
