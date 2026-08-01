# Brainstorming: Eino Tooling Integration

**Date:** 2026-03-01

## Goal
Complete Eino ADK integration with proper tool support for LLM agents.

## Context
- `ADKAgent` exists with Eino ADK ChatModelAgent + Runner integration
- `WSConnRef` for swappable websocket connections
- Global tools: DuckDuckGo search from eino-ext
- `AgentManager` manages per-user agents with thread-safe access
- Session handler uses lock pattern for agent creation
- Mem0 handled automatically via ChatTemplate (not as tools)
- Documentation created: docs/internal/agent.md, docs/internal/websocket/agent_manager.md

## Implementation Status
All core features implemented and documented:
- ✅ ADKAgent with Eino ChatModelAgent and Runner
- ✅ WSConnRef for swappable websocket connections
- ✅ Loop with message coalescing
- ✅ processTasks with message combining
- ✅ GetGlobalTools with DuckDuckGo search tool
- ✅ buildInstruction with mem0 primacy
- ✅ Session handler with lock pattern
- ✅ Comprehensive unit tests
- ✅ Integration tests
- ✅ Documentation in proper format

## Deferred (Documented Only)
- Node tools - Remote execution on user devices (documented but not implemented)
- MCP tools - Model Context Protocol tools (documented but not implemented)
- Additional global tools beyond DuckDuckGo
- Streaming tool results (ExecuteStreaming)
- Tool discovery protocol
- Tool permissions system
