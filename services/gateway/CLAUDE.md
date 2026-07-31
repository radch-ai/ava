# Claude Code Context - gateway

## Tech Stack

- **Language**: Go

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

- WebSocket gateway with LLM agent integration
- Multi-provider LLM client (OpenAI, Anthropic, Ollama, Azure)
- Tool execution and agent runtime
