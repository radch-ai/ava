# Eino Tooling Integration Implementation Plan

> **Implementation Status: COMPLETE**
> All core features have been implemented, tested, and documented.
> See: `docs/internal/agent.md` and `docs/internal/websocket/agent_manager.md`
> Commits: `b3ee7aa` through `8cb02f3` (12 commits)

## Deferred (Not Yet Implemented)

### Node Tools
Remote execution on user devices - documented in brainstorming but not implemented.

**Design Considerations:**
- Node devices can register their own tools
- Tools are per-user, scoped by device ID
- Discovery protocol needed for tool registration
- Not included in initial MVP

### MCP Tools
Model Context Protocol tools - documented in brainstorming but not implemented.

**Design Considerations:**
- MCP servers provide tool definitions dynamically
- Tools can be loaded at runtime
- Not included in initial MVP

### Tool Discovery Protocol
WebSocket API to expose available tools to clients - documented but not implemented.

**Design Considerations:**
- ToolsList event with tool names and schemas
- Used by clients to build tool choosers
- Not included in initial MVP

### Streaming Tool Results
ExecuteStreaming for long-running operations - documented but not implemented.

**Design Considerations:**
- Tools can stream partial results back
- Progress events during tool execution
- Not included in initial MVP

### Tool Permissions System
Authorization system for tool access - documented but not implemented.

**Design Considerations:**
- Users must grant permission for tools to access resources
- Not included in initial MVP

### Additional Global Tools
Beyond DuckDuckGo search - documented but not implemented.

**Examples:** WolframAlpha, Wikipedia, weather APIs, etc.

**Design Considerations:**
- Each tool requires API key or configuration
- Should be configured per-user or per-agent
- Not included in initial MVP

