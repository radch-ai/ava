# Gateway Implementation Plan

> **Workflow:** When a phase is complete:
> 1. Move details from brainstorming → `docs/` (proper project documentation)
> 2. Remove from this file only what's been completed
> 3. Keep only pending/in-progress work here

> **Phase 0: Foundation** → See [`docs/foundation.md`](../docs/foundation.md)

---

## Phase 1: Core Gateway - MVP

**Status:** ✅ COMPLETE

All Phase 1 goals have been implemented and documented:

### ✅ Message Protocol

**Files:** `internal/websocket/handler.go`, `docs/websocket/protocol.md`

- ✅ Message JSON schema defined
- ✅ Message types: prompt, auth, error, heartbeat
- ✅ Message validation implemented
- ✅ Error response format
- ✅ Version field for protocol evolution

### ✅ Connection Management

**Files:** `internal/websocket/handler.go`, `docs/websocket/handler.md`

- ✅ Connection registry (active connections map)
- ✅ Connection limiting (max concurrent connections)
- ✅ Heartbeat/ping-pong coordination
- ✅ Graceful connection cleanup
- ✅ Single-writer pattern for thread safety

### ✅ Authentication

**Note:** This phase was simplified. OAuth/JWT moved to future consideration.

**Current Status:**
- ✅ Basic WebSocket handshake
- ✅ Session-based agent management (per connection)
- ✅ Connection tracking with unique IDs

### ✅ Tool Routing

**Files:** `internal/agent/`, `internal/agent/tools/`, `docs/agent/`

- ✅ Tool invocation protocol via agent tool calling loop (see `internal/agent/agent.go:192-252`)
- ✅ Tool execution framework with timeout handling
- ✅ Tool registry with registration/lookup
- ✅ Example tools implemented (echo, math, time, delay)

---

## Phase 2: Agent Runtime (Ported from pi-agent-core)

**Status:** ✅ COMPLETE

**Files:** `internal/agent/*.go`, `docs/agent/`

All agent components implemented:
- ✅ Core types (Role, ContentBlock, AgentMessage, Model, Tool, ToolResult) - see `internal/agent/types.go`
- ✅ Agent state management with thread-safe access - see `internal/agent/types.go`
- ✅ Event streaming (10 event types) - see `internal/agent/events.go`
- ✅ Agent struct with NewAgent, Subscribe, Unsubscribe, State access - see `internal/agent/agent.go`
- ✅ Tool calling loop - see `internal/agent/agent.go:192-252` (currently mock implementation)
- ✅ Mock LLM client for testing - see `internal/agent/mock.go`

**Note:** The `runToolCallingLoop()` function at lines 224-252 of `internal/agent/agent.go` is a **placeholder** that returns "Simulated response to: [user text]". This needs to be replaced with real LLM streaming in Phase 3.

---

## Phase 3: LLM Integration

**Status:** 🔄 IN PROGRESS

**Files:** `internal/llm/*.go`, `docs/llm/`

Completed:
- ✅ Anthropic client with official SDK (`github.com/anthropics/anthropic-sdk-go` v1.22.1)
- ✅ OpenAI client with official SDK (`github.com/openai/openai-go` v1.12.0)
- ✅ Google client with official SDK (`google.golang.org/genai` v1.46.0)
- ✅ Factory pattern with provider auto-detection
- ✅ Tool format conversion to provider-specific formats

Remaining:
- ❌ Wire LLM clients into Agent.Prompt() - replace mock `runToolCallingLoop()`
- ❌ Add environment variable configuration (API keys, default model)
- ❌ Create adapter between LLM client and agent interfaces
- ❌ End-to-end integration testing

**Plan:** See `.claude/plans/2026-02-11-llm-agent-integration.md`

---

## References

- Documentation: `docs/`
- Architecture decisions: `.claude/brainstorming/gateway-architecture-decisions.md`
- Agent runtime notes: `.claude/brainstorming/agent-runtime-llm-proxy.md`
