# Gateway Architecture Decisions

**Date:** 2025-01-15 (original)
**Last Updated:** 2026-02-11

---

## Completed Architecture Decisions

The following architecture decisions have been **implemented**:

### ✅ Single Service Architecture (Part 3)

**Decision:** Port pi-agent-core to Go instead of separate Node.js service

**Benefits achieved:**
- ✅ Master LLM API key stays in one service (no cross-service exposure)
- ✅ Zero inter-service latency (in-process calls)
- ✅ Single binary deployment
- ✅ Simpler debugging and tracing
- ✅ Go-only codebase

**Implementation Status:**
- ✅ `internal/agent/` - Ported agent core from pi-agent-core
- ✅ `internal/llm/` - LLM proxy with official SDKs (not porting from pi-ai)
- ✅ `internal/websocket/` - WebSocket server for client connections
- ✅ `internal/server/` - HTTP server with graceful shutdown
- ✅ `internal/config/` - Environment-based configuration

### ✅ Tool Execution Model

**Decision:** Centralized tools with per-session rate limiting

**Status:**
- ✅ `internal/agent/tools/` - Tool registry with example tools
- ✅ Per-connection agent instances (via AgentManager)

### ✅ LLM Proxy with Official SDKs

**Decision:** Use official Go SDKs instead of porting pi-ai's 10,000+ lines

**Providers implemented:**
- ✅ Anthropic - `github.com/anthropics/anthropic-sdk-go` v1.22.1
- ✅ OpenAI - `github.com/openai/openai-go` v1.12.0
- ✅ Google - `google.golang.org/genai` v1.46.0

**Documentation:** `docs/llm/` (7 files)

---

## Remaining Tasks

### ❌ LLM Integration Into Agent

**Status:** NOT STARTED

The agent has all the infrastructure but still uses mock `runToolCallingLoop()`.

**What's Needed:**
1. Add `LLMClient` interface to agent package
2. Wire LLM client into Agent.Prompt() method
3. Replace mock responses with real LLM streaming
4. Configure LLM API keys from environment

**Plan:** See `.claude/plans/2026-02-11-llm-agent-integration.md`

---

## References

- Documentation: `docs/`
- LLM integration plan: `.claude/plans/2026-02-11-llm-agent-integration.md`
- Agent runtime notes: `.claude/brainstorming/agent-runtime-llm-proxy.md`
- Archived discussions: `.claude/brainstorming/gateway-architecture-decisions-archive.md`
