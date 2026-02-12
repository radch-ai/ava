# Claude Code Context - gateway

## Tech Stack

- **Language**: Go

## Quick Commands

```bash
# Build
make build

# Run
make run

# Test
make test

# Regenerate project details from parent
make generate
```

## Project Documentation

See the `project-workflow` skill for documentation structure (brainstorming/, plans/, docs/) and TDD workflow.

## Architecture Notes

- WebSocket gateway with LLM agent integration
- Multi-provider LLM client (OpenAI, Anthropic, Ollama, Azure)
- Tool execution and agent runtime
