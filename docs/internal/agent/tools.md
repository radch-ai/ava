# Agent Tools

Built-in agent tools and tool registry for the agent runtime.

## Overview

```mermaid
flowchart TB
    subgraph Tools ["Built-in Tools"]
        Echo["ExampleEchoTool()
        📄 example.md"]
        Math["ExampleMathTool()
        📄 example.md"]
        Time["ExampleTimeTool()
        📄 example.md"]
        Delay["ExampleDelayTool()
        📄 example.md"]
        Files["ReadAgentFileTool()
        WriteAgentFileTool()
        📄 files.md"]
        Mem0["Mem0SearchTool()
        Mem0StoreTool()
        📄 mem0.md"]
        Node["NodeToolRegistry
        📄 node.md"]
    end
    
    Registry["NewRegistry()
    📄 registry.md"]
    
    Echo --> Registry
    Math --> Registry
    Time --> Registry
    Delay --> Registry
    Files --> Registry
    Mem0 --> Registry
    Node --> Registry

    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    
    class Echo,Math,Time,Delay,Files,Mem0,Node,Registry localPkgs
```

## Files

| File | Description |
|------|-------------|
| [`registry.go`](../../internal/agent/tools/registry.go) | Tool registry for registration and lookup |
| [`example.go`](../../internal/agent/tools/example.go) | Example tools (echo, math, time, delay) |
| [`files.go`](../../internal/agent/tools/files.go) | Agent workspace file operations |
| [`mem0.go`](../../internal/agent/tools/mem0.go) | Long-term memory tools |
| [`node.go`](../../internal/agent/tools/node.go) | Remote node tool integration |

## Tool Categories

### Example Tools

| Tool | Description |
|------|-------------|
| `echo` | Echoes input text back |
| `math` | Performs simple math operations |
| `time` | Returns current time |
| `delay` | Waits for specified duration |

### File Tools

| Tool | Description |
|------|-------------|
| `read_agent_file` | Read from agent workspace |
| `write_agent_file` | Write to agent workspace |

### Memory Tools

| Tool | Description |
|------|-------------|
| `mem0_search` | Search stored memories |
| `mem0_store` | Store in long-term memory |

### Node Tools

| Pattern | Description |
|---------|-------------|
| `{platform}_{nodeID}_{toolName}` | Execute on node in Docker |
| `{platform}_{nodeID}_{toolName}_onhost` | Execute on node directly |

## Registry

### Structs

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Registry` | `mu` | `sync.RWMutex` | Thread-safe lock |
| | `tools` | `map[string]agent.Tool` | Tool storage |

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewRegistry` | `(tools ...agent.Tool) *Registry` | Creates registry |
| `RegisterTool` | `(r *Registry, name string, tool agent.Tool) bool` | Registers tool |
| `GetTool` | `(r *Registry, name string) *agent.Tool` | Gets tool by name |
| `DefaultTools` | `() []agent.Tool` | Returns example tools |

## Design Notes

### Thread-Safe Registry

**Q: Why use sync.RWMutex instead of sync.Map?**

| Aspect | Description |
|--------|-------------|
| Context | Tool registry concurrency |
| Options considered | sync.Map, sync.RWMutex, channel-based |
| Rationale | RWMutex is simpler for read-heavy pattern |

**Current approach is recommended because:**
- Reads don't block each other
- Simple lock/unlock pattern
- Clear critical sections
- Standard Go pattern

### Tool Name Prefix for Removal

**Q: Why remove tools by prefix instead of exact name?**

| Aspect | Description |
|--------|-------------|
| Context | Dynamic tool management |
| Options considered | Exact name, prefix, tag-based |
| Rationale | Node tools share platform_nodeID prefix |

**Current approach is recommended because:**
- Efficient batch removal
- Groups related tools
- Works well with node lifecycle
- Simple string comparison

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestRegistry` | Tool registration and lookup |
| `TestExampleEchoTool` | Echo tool execution |
| `TestExampleMathTool` | Math operations |
| `TestExampleTimeTool` | Time tool |
| `TestExampleDelayTool` | Delay with context |
| `TestFileTools` | File read/write |
| `TestMem0Tools` | Memory search/store |
| `TestNodeToolRegistry` | Node tool management |

**Running:**
```bash
make test ./internal/agent/tools
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `sync` | Thread safety |
| `context` | Context handling |
| `internal/agent` | Agent types |
| `internal/ctx` | Context utilities |
| `internal/storage` | S3 client |
| `internal/mem0` | Memory client |
| `internal/node` | Node types |

**File Locations:**
- [`internal/agent/tools/registry.go`](../../internal/agent/tools/registry.go)
- [`internal/agent/tools/example.go`](../../internal/agent/tools/example.go)
- [`internal/agent/tools/files.go`](../../internal/agent/tools/files.go)
- [`internal/agent/tools/mem0.go`](../../internal/agent/tools/mem0.go)
- [`internal/agent/tools/node.go`](../../internal/agent/tools/node.go)

## Related

- [Agent README](README.md) - Main agent package
- [Storage](../storage/README.md) - Workspace storage
- [Mem0](../mem0/README.md) - Memory service
- [Node](../node/README.md) - Node service
