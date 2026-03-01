# Brainstorming: Eino Tooling Integration

**Date:** 2026-03-01

## Goal

Complete Eino ADK integration with proper tool support for LLM agents.

## Context

### Current State
- `EinoAgentImpl` exists with custom interfaces (`EinoRunner`, `EinoAgent`)
- Custom `agent.Tool` type in `internal/agent/types.go`
- Custom tool registry in `internal/agent/tools/registry.go`
- `QuotaChatModel` wrapper exists
- `ProgressCallbacks` exists for WebSocket events
- No real Eino ADK `ChatModelAgent` or `Runner` being used

### Gaps Identified
- Eino ADK's `ChatModelAgent` + `Runner` not actually used
- Tools not connected to Eino's `ToolsNode`
- No tool schema definitions for LLM

## Questions Explored

### Q1: Focus Area
Options: JSON Schema for Tools, Eino ADK Integration, Tool Discovery, Streaming
**Decision:** Eino ADK Integration

### Q2: Tool Approach
Options: Wrapper Pattern, Direct Eino Tools, Hybrid
**Decision:** Direct Eino Tools - use `eino/components/tool` like `eino/components/model`

### Q3: Tools to Build
- Not mem0 tools - mem0 handled automatically via ChatTemplate
- Global tools: DuckDuckGo search from `eino-ext`
- Node tools: Documented but deferred

### Q4: Architecture
- No separate AgentManager (was for multi-agent per socket)
- Now: HTTP/2, one agent per socket
- Agent is independent, survives socket disconnect
- WSConnRef for swappable connection

### Q5: Initialization Order
```
Session Handler:
1. mu.Lock()
2. if agent == nil:
      agent = NewAgent(firstMsg)  // creates laneCh, adds msg
      mu.Unlock()
      agent.Start()               // sync init + goroutine
      return
   else:
      agent.laneCh <- task
      mu.Unlock()
```

### Q6: Idle Eviction
Lock coordinates exit - goroutine acquires lock, checks laneCh empty, then exits

### Q7: Mem0 Handling
Not as tools - automatic via ChatTemplate:
- Session start: mem0 memories as message[0]
- Pre-compaction: strip memories, store to mem0, compact clean conversation
- Post-compaction: re-inject memories at message[0]

## Decisions Made

1. **Tool interface:** `tool.InvokableTool` from `eino/components/tool`
2. **Global tools:** Only DuckDuckGo search for MVP
3. **Agent owns:** laneCh, WSConnRef, einoAgent, einoRunner
4. **Session handler:** Creates agent, coordinates via lock
5. **WSConnRef:** Simple Get/Set/Clear/IsSet, no WriteJSON
6. **Callbacks:** Use `connRef.Get()`, existing nil handling works
