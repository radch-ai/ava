# Agent Runtime & LLM Proxy - Architecture & Implementation

**Date:** 2026-02-11
**Status:** Implementation In Progress

> **This document tracks agent runtime, LLM integration, and remaining work.**

---

## What's Been Implemented

### ✅ Agent Runtime (Ported from pi-agent-core)

**Files:** `internal/agent/types.go`, `internal/agent/events.go`, `internal/agent/agent.go`, `internal/agent/loop.go`, `internal/agent/tool.go`, `internal/agent/mock.go`

**Documentation:** `docs/agent/` (10 files)

All core types and functionality:
- ✅ Role, ContentBlock types (TextContent, ImageContent, ToolCallContent)
- ✅ AgentMessage with role, content, timestamp, tool result fields
- ✅ Model, ThinkingLevel, Tool, ToolResult types
- ✅ AgentState with AddMessage, SetStreaming, tool call tracking
- ✅ Agent struct with NewAgent, Subscribe, Unsubscribe, State access
- ✅ 10 event types (AgentStart/End, TurnStart/End, MessageStart/Update/End, ToolExecutionStart/Update/End)
- ✅ Loop struct with Run, ExtractToolCalls, AddToolResults
- ✅ ExecuteToolCall, FindToolByName, ToolExecutionError
- ✅ MockLLMClient and test helpers
- ✅ Tools subpackage with registry and example tools

### ✅ LLM Clients (Using Official SDKs)

**Files:** `internal/llm/*.go`

**Documentation:** `docs/llm/` (7 files)

All three providers implemented:
- ✅ Anthropic - `github.com/anthropics/anthropic-sdk-go` v1.22.1
- ✅ OpenAI - `github.com/openai/openai-go` v1.12.0
- ✅ Google - `google.golang.org/genai` v1.46.0
- ✅ Factory pattern with provider auto-detection
- ✅ Tool format conversion (JSON Schema to provider formats)

### ✅ WebSocket Handler

**Files:** `internal/websocket/handler.go`, `internal/websocket/agent_manager.go`

**Documentation:** `docs/websocket/` (4 files)

- ✅ WebSocket connection management with connection limiting
- ✅ Per-connection agent instances (AgentManager)
- ✅ Graceful shutdown and single-writer pattern
- ✅ Event forwarding from agent to WebSocket clients

### ✅ Server

**Files:** `internal/server/*.go`

**Documentation:** `docs/server/` (2 files)

- ✅ HTTP server with graceful shutdown
- ✅ Health check endpoint at `/health`
- ✅ WebSocket endpoint at `/ws`

### ✅ Config

**Files:** `config/*.go`

**Documentation:** `docs/config.md`

- ✅ Environment variable configuration (GATEWAY_ADDR, GATEWAY_LOG_LEVEL, etc.)
- ✅ Model and router configuration from YAML
- ✅ Subscription plan definitions

---

## LLM Proxy vs Direct Calls - Architecture Decision

### Original Consideration

We had a key question about the LLM integration approach:

**Option A: LLM Proxy Layer**
- Master LLM API key in gateway
- Rate limiting per user/session
- Usage tracking and billing enforcement
- Extra abstraction layer

**Option B: Direct LLM Calls**
- Call official SDKs directly from Agent
- Simpler architecture
- Better observability
- Less latency

### Decision Made

We chose **Option B: Direct LLM Calls with Official SDKs**

**Rationale:**
1. **Simpler** - No need for proxy abstraction when using official SDKs
2. **Better error handling** - SDKs handle retries, backoff, error codes
3. **Type safety** - Official SDKs provide proper types
4. **Updates maintained** - SDK updates are just dependency updates
5. **Rate limiting can be added later** - Can implement at agent/gateway level if needed
6. **Cost tracking optional** - Usage tracking is a "nice to have" not "must have now"

**What this means:**
- No separate LLM proxy layer with usage tracking
- Agent calls LLM clients directly via factory pattern
- Rate limiting/billing is a future consideration, not gating feature

---

## Remaining Tasks

### ❌ LLM Integration Into Agent

**Status:** NOT STARTED

**Current State:**
- `Agent.Prompt()` at `internal/agent/agent.go:174-209` calls mock `runToolCallingLoop()`
- Mock loop returns "Simulated response to: [user text]"
- `Loop.Run()` exists at `internal/agent/loop.go:35-93` and provides tool calling but uses mock LLM client

**What's Needed:**
1. Add `LLMClient` interface to agent package
2. Add `LLMClient` field to `AgentOptions` and `Agent` struct
3. Modify `Agent.Prompt()` to use `Loop.Run()` instead of mock `runToolCallingLoop()`
4. Create adapter between LLM client events and agent StreamEvent type
5. Wire LLM client into agent creation in WebSocket handler
6. Add environment variable configuration for LLM API keys
7. End-to-end integration testing

**Plan:** See `.claude/plans/2026-02-11-llm-agent-integration.md`

---

## References

- pi-agent-core: https://github.com/badlogic/pi-mono/tree/main/packages/agent
- Documentation: `docs/`
- LLM integration plan: `.claude/plans/2026-02-11-llm-agent-integration.md`
