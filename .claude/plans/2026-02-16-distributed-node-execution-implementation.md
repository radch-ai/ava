# Distributed Node Execution - Implementation Plan

**Status:** Ready
**Created:** 2026-02-16
**Design:** [brainstorming/2026-02-16-distributed-node-execution.md](../brainstorming/2026-02-16-distributed-node-execution.md)

## Overview

Implement distributed node execution system allowing agent tools to run on user devices instead of VPS infrastructure. The gateway orchestrates tool execution across nodes while maintaining security and plan-based limits.

## Architecture Summary

```
User Node 1          User Node N          Gateway
   |                    |                   |
   |--WebSocket----------|-------------------|
   |                    |                   |
   |--Register Tools----|-------------------|
   |<--Assigned ID------|                   |
   |                    |                   |
   |--Tool Request------|                   |
   |<--Tool Result------|                   |
                               |
                        Agent Manager
                               |
                        Agent + Tools
                               |
                        Tool Registry
                               |
                        Tool Fetcher
                      (Node + Cloud)
                               |
                         Redis Cache
```

## Tool Naming Convention

```
{platform}_{node_uuid}_{tool_name}[_variant]

Examples:
- linux_abc123-weather_current    # Linux node weather tool
- darwin_def456-fs_read            # macOS node file system read
- windows_789xyz-shell_exec       # Windows node shell tool
```

## Implementation Phases

### Phase 1: Gateway Node Protocol

**Goal:** Establish core types and WebSocket protocol for node registration and tool execution.

#### Task 1.1: Node Protocol Types
**File:** `internal/node/types.go`

**Types to create:**
- `NodeType`: Private vs Managed
- `NodeInfo`: node_uuid, platform, hostname, user_id, type
- `ToolDefinition`: name, description, parameters (JSON Schema)
- `ToolCallRequest`: tool_name, arguments, call_id
- `ToolCallResponse`: call_id, result, error
- `NodeMessage`: wrapper for all WebSocket messages (type, data)
- `NodeRegistry`: thread-safe map of registered nodes

**Tests:**
- `TestNodeType_String`: verify enum values
- `TestNodeInfo_Valid`: valid NodeInfo passes validation
- `TestNodeInfo_MissingUUID`: invalid NodeInfo fails
- `TestToolDefinition_MarshalJSON`: JSON serialization
- `TestToolCallRequest_MarshalJSON`: JSON serialization
- `TestNodeRegistry_RegisterAddNode`: node registration
- `TestNodeRegistry_RegisterDuplicate`: duplicate rejected
- `TestNodeRegistry_RemoveNode`: node removal
- `TestNodeRegistry_GetNode`: retrieve existing node
- `TestNodeRegistry_GetNodeNotFound`: returns nil

---

#### Task 1.2: Node Registry Implementation
**File:** `internal/node/registry.go`

**Functions:**
- `NewRegistry() *Registry`: create new registry
- `RegisterNode(node NodeInfo) error`: register node, return error if duplicate
- `RemoveNode(nodeUUID string)`: remove node from registry
- `GetNode(nodeUUID string) *NodeInfo`: get node by UUID
- `GetNodesByUserID(userID string) []NodeInfo`: get all user's nodes
- `GetToolsForNode(nodeUUID string) []ToolDefinition`: get node's tools
- `UpdateNodeHeartbeat(nodeUUID string)`: update last seen timestamp
- `GetStaleNodes(timeout time.Duration) []string`: get nodes not seen recently

**Tests:**
- `TestNewRegistry_Empty`: starts empty
- `TestRegisterNode_Success`: adds node
- `TestRegisterNode_DuplicateUUID`: returns error
- `TestRemoveNode_Existing`: removes node
- `TestRemoveNode_NonExisting`: no error
- `TestGetNode_Existing`: returns node
- `TestGetNode_NonExisting`: returns nil
- `TestGetNodesByUserID_Empty`: no nodes
- `TestGetNodesByUserID_Multiple`: returns all user nodes
- `TestGetToolsForNode_WithTools`: returns tool list
- `TestGetToolsForNode_NoTools`: returns empty slice
- `TestUpdateNodeHeartbeat_Existing`: updates timestamp
- `TestUpdateNodeHeartbeat_NonExisting`: no-op
- `TestGetStaleNodes_None`: no stale nodes
- `TestGetStaleNodes_Some`: returns stale node UUIDs

---

#### Task 1.3: Node WebSocket Handler
**File:** `internal/node/handler.go`

**Functions:**
- `NewHandler(logger, registry, llmProxy) *Handler`
- `HandleConnection(conn, userID)`: main connection handler
- `handleRegister(msg)`: register node and assign tools
- `handleToolCall(msg)`: execute tool on node, wait for response
- `handleHeartbeat(msg)`: update node heartbeat
- `handleUnregister(msg)`: remove node from registry
- `sendToolCall(conn, toolCall, timeout)`: send tool call request

**Message Types:**
- `register`: NodeInfo + ToolDefinition[]
- `tool_call_request`: ToolCallRequest (gateway → node)
- `tool_call_response`: ToolCallResponse (node → gateway)
- `heartbeat`: node_uuid
- `unregister`: node_uuid

**Tests:**
- `TestNewHandler_CreatesHandler`: valid handler created
- `TestHandleConnection_RegisterSuccess`: node registers
- `TestHandleConnection_RegisterInvalid`: rejected with error
- `TestHandleConnection_ToolCallSuccess`: tool executed
- `TestHandleConnection_ToolCallTimeout`: timeout error
- `TestHandleConnection_ToolCallNotFound`: tool not found error
- `TestHandleConnection_HeartbeatUpdates`: heartbeat timestamp updated
- `TestHandleConnection_UnregisterRemoves`: node removed from registry

---

#### Task 1.4: WebSocket Server Route
**File:** `internal/websocket/node_routes.go`

**Functions:**
- `RegisterNodeRoutes(router, handler)`: register `/node` WebSocket endpoint
- `NodeUpgradeMiddleware`: verify auth token, extract userID

**Integration points:**
- Use existing `websocket.Upgrader`
- Use existing `AuthMiddleware` for authentication
- Add `/ws/node` endpoint

**Tests:**
- `TestRegisterNodeRoutes_RouteRegistered`: route exists
- `TestNodeUpgradeMiddleware_ValidToken`: upgrades connection
- `TestNodeUpgradeMiddleware_InvalidToken`: returns 401
- `TestNodeUpgradeMiddleware_NoToken`: returns 401
- `TestNodeWebSocket_RegisterFlow`: end-to-end registration

---

### Phase 2: Agent Integration

**Goal:** Integrate node tools with agent runtime, combining node and cloud tools.

#### Task 2.1: Agent Tools Support
**File:** `internal/agent/agent.go` (modify)

**Changes to `AgentOptions`:**
- Add `NodeTools []Tool`: tools available from nodes
- Add `ToolFetcher func() []Tool`: dynamic tool fetcher

**Changes to `Agent`:**
- Store tool fetcher if provided
- Call fetcher when building tool list for LLM

**Tests:**
- `TestNewAgent_WithNodeTools`: node tools included
- `TestNewAgent_WithToolFetcher`: fetcher called
- `TestNewAgent_WithBoth`: both static and fetched tools
- `TestAgent_GetTools_CallsFetcher`: fetcher invoked

---

#### Task 2.2: Tool Cache
**File:** `internal/agent/tool_cache.go`

**Types:**
- `CachedToolList`: tools, timestamp, node_count
- `ToolCache`: TTL-based cache for tool lists

**Functions:**
- `NewToolCache(ttl time.Duration) *ToolCache`: create cache
- `Get(key string) *CachedToolList`: get cached tools
- `Set(key string, tools []Tool)`: cache tool list
- `Invalidate(key string)`: remove specific cache entry
- `Clear()`: clear all cache

**Tests:**
- `TestNewToolCache_CreatesCache`: valid cache
- `TestToolCache_GetMiss`: returns nil
- `TestToolCache_SetAndGet`: returns cached value
- `TestToolCache_Expiration`: expired entry returns nil
- `TestToolCache_Invalidate`: removes entry
- `TestToolCache_Clear`: empties cache

---

#### Task 2.3: Tool Fetcher
**File:** `internal/agent/tool_fetcher.go`

**Types:**
- `ToolFetcherOptions`: registry, redis cache, logger

**Functions:**
- `NewToolFetcher(opts ToolFetcherOptions) *ToolFetcher`
- `FetchTools(userID string) []Tool`: combine node + cloud tools
- `fetchNodeTools(userID) []Tool`: get tools from user's nodes
- `fetchCloudTools() []Tool`: get static cloud tools
- `getFromCache(userID) *CachedToolList`: check Redis cache
- `setCache(userID, tools)`: store in Redis

**Tests:**
- `TestNewToolFetcher_CreatesFetcher`: valid fetcher
- `TestFetchTools_WithNodes`: includes node tools
- `TestFetchTools_NoNodes`: only cloud tools
- `TestFetchTools_CacheHit`: uses cached tools
- `TestFetchTools_CacheMiss`: fetches fresh
- `TestFetchNodeTools_NoNodes`: empty list
- `TestFetchNodeTools_WithNodes`: returns tools
- `TestFetchNodeTools_NameFormat`: correct naming convention

---

### Phase 3: Tool Caching & Health

**Goal:** Implement Redis caching and health discovery for nodes.

#### Task 3.1: Health Endpoint
**File:** `internal/node/health.go`

**Functions:**
- `CheckNodeHealth(conn, timeout) error`: ping node via WebSocket
- `MarkUnhealthy(nodeUUID)`: mark node as unhealthy
- `IsHealthy(nodeUUID) bool`: check health status

**Tests:**
- `TestCheckNodeHealth_Healthy`: returns nil on pong
- `TestCheckNodeHealth_Timeout`: returns error
- `TestMarkUnhealthy_SetsStatus`: status changes
- `TestIsHealthy_True`: healthy node returns true
- `TestIsHealthy_False`: unhealthy node returns false

---

#### Task 3.2: Refresh Triggers
**File:** `internal/node/refresh.go`

**Functions:**
- `TriggerRefresh(userID, nodeUUID)`: request tool refresh from node
- `HandleToolListUpdate(msg)`: process updated tool list from node
- `InvalidateUserCache(userID)`: clear user's tool cache

**Tests:**
- `TestTriggerRefresh_SendsMessage`: refresh message sent
- `TestHandleToolListUpdate_UpdatesRegistry`: tools updated
- `TestHandleToolListUpdate_InvalidatesCache`: cache cleared
- `TestInvalidateUserCache_RemovesEntry`: cache entry gone

---

### Phase 4: Plan Limits Enforcement

**Goal:** Enforce plan-based limits for node counts and tools.

#### Task 4.1: Plan Limits Config
**File:** `internal/config/limits.go`

**Types:**
- `PlanLimits`: max_nodes, max_tools_per_node, max_concurrent_executions
- `PlanTier`: Lite, Standard, Pro with preset limits

**Functions:**
- `GetLimits(tier PlanTier) PlanLimits`: get limits for tier
- `ValidateNodeCount(userID, tier) error`: check node count limit
- `ValidateToolCount(nodeUUID, tier) error`: check tool count per node

**Default Limits:**
- Lite ($20): 2 nodes, 10 tools/node, 2 concurrent
- Standard ($50): 5 nodes, 20 tools/node, 5 concurrent
- Pro ($100): 15 nodes, 50 tools/node, 15 concurrent

**Tests:**
- `TestGetLimits_Lite`: correct limits
- `TestGetLimits_Standard`: correct limits
- `TestGetLimits_Pro`: correct limits
- `TestValidateNodeCount_UnderLimit`: no error
- `TestValidateNodeCount_AtLimit`: no error
- `TestValidateNodeCount_OverLimit`: returns error
- `TestValidateToolCount_UnderLimit`: no error
- `TestValidateToolCount_OverLimit`: returns error

---

#### Task 4.2: Limits Validation in Handler
**File:** `internal/node/handler.go` (modify)

**Changes:**
- Add `limits PlanTier` to Handler
- Validate node count before registration
- Validate tool count before registration
- Return descriptive error messages

**Tests:**
- `TestHandleConnection_NodeLimitExceeded`: rejected with error
- `TestHandleConnection_ToolLimitExceeded`: rejected with error
- `TestHandleConnection_WithinLimits`: succeeds

---

## Implementation Order

1. **Phase 1** (Days 1-2): Core protocol and registry
2. **Phase 2** (Day 3): Agent integration
3. **Phase 3** (Day 4): Caching and health
4. **Phase 4** (Day 5): Plan limits

## Testing Strategy

- Each task includes specific TDD tests
- Run `make test` after each task completion
- Integration tests for WebSocket flows
- Mock node connections for handler tests

## Dependencies

- `github.com/gorilla/websocket`: existing
- `github.com/redis/go-redis/v9`: existing
- `github.com/rs/zerolog`: existing
- No new external dependencies required
