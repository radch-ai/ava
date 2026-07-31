# Gateway Brainstorming Archive

**Date:** 2026-02-11
**Purpose:** Archived design explorations, architecture decisions, and implementation plans from early gateway development.

> **Note:** This file preserves historical context. Current active work is in `.claude/plans/2026-02-11-llm-agent-integration.md`

---

## Architecture Decisions

### Single Service vs Two Services

**Decision:** Use single Go service instead of separate pi-agent Node.js service

**Rationale:**
- Master API key stays in one service (no cross-service exposure)
- Zero inter-service latency (in-process calls)
- Single binary deployment
- Simpler debugging and tracing
- Go-only codebase

### Direct LLM Calls vs Proxy Layer

**Decision:** Call LLM providers directly using official SDKs, not build a proxy layer

**Rationale:**
1. **Simpler** - No need for proxy abstraction when using official SDKs
2. **Better error handling** - SDKs handle retries, backoff, error codes
3. **Type safety** - Official SDKs provide proper types
4. **Updates maintained** - SDK updates are just dependency updates
5. **Rate limiting optional** - Usage tracking is "nice to have" not "must have now"
6. **Cost tracking optional** - Billing is a future consideration

**Result:** Built LLM clients using:
- `github.com/anthropics/anthropic-sdk-go` v1.22.1
- `github.com/openai/openai-go` v1.12.0
- `google.golang.org/genai` v1.46.0

---

## What Was Implemented

The following components from original brainstorming were successfully implemented:

### Agent Runtime (from pi-agent-core)
- All message types (Role, ContentBlock, AgentMessage, Model, ThinkingLevel)
- AgentState management with AddMessage, SetStreaming
- Agent struct with Subscribe/Unsubscribe event broadcasting
- 10 event types for lifecycle tracking
- Tool execution framework with ExecuteToolCall, FindToolByName
- Mock LLM client for testing
- Loop struct with Run, ExtractToolCalls, AddToolResults
- Tools subpackage with registry and example tools

### LLM Integration
- Three provider clients using official SDKs
- Factory pattern with provider auto-detection
- Tool format conversion to provider-specific formats

### WebSocket Handler
- WebSocket connection management
- Per-connection agent instances (AgentManager)
- Graceful shutdown and single-writer pattern
- Event forwarding from agent to WebSocket clients
- Connection limiting

### Server
- HTTP server with graceful shutdown
- Health check endpoint `/health`
- WebSocket endpoint `/ws`

### Config
- Environment variable configuration (GATEWAY_ADDR, GATEWAY_LOG_LEVEL, etc.)

---

## Tools Referenced

- pi-agent-core: https://github.com/badlogic/pi-mono/tree/main/packages/agent
- OpenClaw: https://github.com/openclaw/openclaw
- Service architecture: Historical (archived separately)

---

## Current Status (2026-02-11)

### ✅ Complete
- Agent Runtime
- LLM Clients (all 3 providers)
- WebSocket Handler
- Server
- Config

### 🔄 In Progress
- **LLM Integration into Agent** - See `.claude/plans/2026-02-11-llm-agent-integration.md`

The agent still uses mock `runToolCallingLoop()` and needs to be wired to real LLM clients.
