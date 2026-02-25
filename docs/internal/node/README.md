# Node

Distributed node execution for running tools on remote devices and managed servers.

## Overview

```mermaid
flowchart TB
    subgraph NodeConnection ["Node Connection"]
        Register["RegisterConnection(ctx, conn, nodeID, userID, platform, tools)"]
        Unregister["UnregisterConnection(ctx, nodeID, userID)"]
    end

    subgraph ToolExecution ["Tool Execution"]
        Execute["ExecuteOnNode(ctx, nodeID, toolName, args)"]
        SendMsg["Send ExecuteMessage"]
        WaitResult["Wait for ToolResultMessage"]
    end

    subgraph Registry ["Node Registry"]
        RegisterNode["RegisterNode(ctx, userID, nodeID, platform, tools, conn)"]
        GetNode["GetNode(nodeID) → NodeInfo"]
        UnregisterNode["UnregisterNode(ctx, nodeID)"]
    end

    Register --> Registry
    Execute --> SendMsg --> WaitResult
    Unregister --> Registry

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Register,Unregister,Execute,SendMsg,WaitResult,RegisterNode,GetNode,UnregisterNode internalFn
```

## Constants

### MessageType

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `MessageTypeRegister` | `MessageType` | `"register"` | Node registration |
| `MessageTypeHealthResponse` | `MessageType` | `"health_response"` | Health check response |
| `MessageTypeExecute` | `MessageType` | `"execute"` | Tool execution request |
| `MessageTypeToolResult` | `MessageType` | `"tool_result"` | Tool execution result |
| `MessageTypeError` | `MessageType` | `"error"` | Protocol error |

### NodeStatus

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `NodeStatusOnline` | `NodeStatus` | `"online"` | Node is online |
| `NodeStatusBusy` | `NodeStatus` | `"busy"` | Node is busy |
| `NodeStatusHealthy` | `NodeStatus` | `"healthy"` | Node is healthy |
| `NodeStatusDegraded` | `NodeStatus` | `"degraded"` | Node is degraded |
| `NodeStatusUnhealthy` | `NodeStatus` | `"unhealthy"` | Node is unhealthy |
| `NodeStatusOffline` | `NodeStatus` | `"offline"` | Node is offline |

## Structs

### Service

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Service` | `registry` | `*Registry` | Node registry |
| | `executor` | `*Executor` | Tool executor |
| | `agentProvider` | `AgentProvider` | Agent provider |
| | `logger` | `*zerolog.Logger` | Logger |

### NodeInfo

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `NodeInfo` | `NodeID` | `string` | Node identifier |
| | `Platform` | `string` | Platform (mac, windows, etc.) |
| | `UserID` | `string` | User identifier |
| | `ConnectedAt` | `time.Time` | Connection time |
| | `LastSeenAt` | `time.Time` | Last activity |
| | `Tools` | `[]string` | Available tools |
| | `Status` | `NodeStatus` | Node status |

### NodeConnection

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `NodeConnection` | `NodeUUID` | `string` | Node identifier |
| | `Conn` | `WebSocketConn` | WebSocket connection |
| | `SendCh` | `chan []byte` | Send channel |

### ToolSpec

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ToolSpec` | `Name` | `string` | Tool name |
| | `Isolation` | `[]string` | Isolation modes (docker, host) |

### RegisterMessage

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RegisterMessage` | `NodeID` | `string` | Node identifier |
| | `Platform` | `string` | Platform name |
| | `Tools` | `[]ToolSpec` | Tool specifications |

### ExecuteMessage

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ExecuteMessage` | `ToolName` | `string` | Tool to execute |
| | `Arguments` | `map[string]interface{}` | Tool arguments |
| | `CallID` | `string` | Call identifier |
| | `Isolation` | `string` | Isolation mode |

### ToolResultMessage

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ToolResultMessage` | `CallID` | `string` | Call identifier |
| | `Result` | `*agent.ToolResult` | Execution result |
| | `Error` | `*string` | Error message |

## Functions

### Service

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewService` | `(logger *zerolog.Logger, agentProvider AgentProvider) *Service` | Creates service |
| `RegisterConnection` | `(ctx context.Context, conn WebSocketConn, nodeID, userID, platform string, toolSpecs []ToolSpec) error` | Registers node |
| `UnregisterConnection` | `(ctx context.Context, nodeID, userID string) error` | Unregisters node |
| `ExecuteOnNode` | `(ctx context.Context, nodeID, toolName string, args map[string]any) (*agent.ToolResult, error)` | Executes tool |

### Tool Naming

| Function | Signature | Description |
|----------|-----------|-------------|
| `GenerateToolName` | `(platform, nodeID, toolName string, isOnHost bool) string` | Generates full tool name |
| `ToolIdentifier` | `(toolName string) (platform, nodeID, baseTool string, isOnHost bool)` | Parses tool name |
| `IsNodeTool` | `(toolName string) bool` | Checks if node tool |

## Error Handling

| Error Type | Handler | Behavior |
|------------|---------|----------|
| Node not found | Returns error | Tool execution fails |
| Execution timeout | Returns error | 30 second default timeout |
| Connection lost | Unregister node | Tools removed from agent |

## Design Notes

### Tool Name Convention

**Q: Why use {platform}_{nodeID}_{toolName} format?**

| Aspect | Description |
|--------|-------------|
| Context | Tool identification |
| Options considered | UUID, hierarchical, prefix-based |
| Rationale | Human-readable with clear ownership |

**Current approach is recommended because:**
- LLM can understand tool source
- Clear platform differentiation
- Node ID prevents collisions
- Optional _onhost variant for isolation

### Docker vs Host Isolation

**Q: Why support both docker and host execution?**

| Aspect | Description |
|--------|-------------|
| Context | Tool isolation modes |
| Options considered | Docker only, host only, both |
| Rationale | Security default, flexibility when needed |

**Current approach is recommended because:**
- Docker provides safe sandboxing
- Host access for trusted operations
- LLM can choose based on user request
- Clear security boundaries

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestService_RegisterConnection` | Node registration |
| `TestService_UnregisterConnection` | Node removal |
| `TestService_ExecuteOnNode` | Tool execution |
| `TestToolIdentifier` | Name parsing |
| `TestGenerateToolName` | Name generation |

**Running:**
```bash
make test ./internal/node
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `github.com/rs/zerolog` | Logging |
| `internal/agent` | Agent and Tool types |

**File Locations:**
- [`internal/node/service.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/node/service.go) - Node service
- [`internal/node/types.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/node/types.go) - Types and messages
- [`internal/node/registry.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/node/registry.go) - Node registry
- [`internal/node/executor.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/node/executor.go) - Tool executor
- [`internal/node/handler.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/node/handler.go) - WebSocket handler
- [`internal/node/adapter.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/node/adapter.go) - Agent adapter
- [`internal/node/cache.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/node/cache.go) - Response cache

## Related

- [Agent Tools](../agent/tools.md) - Node tool integration
- [WebSocket](../websocket.md) - Connection handling
