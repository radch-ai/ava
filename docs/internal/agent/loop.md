# Agent Loop

Tool calling execution loop. Handles LLM streaming, tool call extraction, and result collection.

## Overview

```mermaid
flowchart TB
    Start("Run(ctx, messages, tools)") --> HRun

    subgraph SGRun [" "]
        HRun["**Run() → *RunResult**"]
        HRun --> CopyMsgs["copy messages"]
        CopyMsgs --> LoopStart{loop}
        LoopStart --> Stream["client.Stream()"]
        Stream -->|error| ErrStream["return error"]
        Stream -->|ok| CollectEvents["collect MessageEndEvent"]
        CollectEvents --> CheckMsg{got message?}
        CheckMsg -->|no| Break["break loop"]
        CheckMsg -->|yes| AppendMsg["append assistant message"]
        AppendMsg --> ExtractTools["ExtractToolCalls()"]
        ExtractTools --> CheckTools{tool calls?}
        CheckTools -->|no| Break2["break loop"]
        CheckTools -->|yes| ExecuteTools["executeTools()"]
        ExecuteTools --> AddResults["AddToolResults()"]
        AddResults --> LoopStart
    end

    Break --> ReturnResult("return RunResult")
    Break2 --> ReturnResult
    ErrStream --> Err("return error")

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnResult mainFn
    class HRun internalFn
    class ErrStream,Err errorFlow
```

## Structs

### Loop

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Loop` | `client` | `LLMClient` | LLM client for streaming |
| | `tools` | `[]Tool` | Available tools |

### RunResult

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RunResult` | `Messages` | `[]AgentMessage` | Final message history |

### ToolExecutionResult

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ToolExecutionResult` | `ToolCallID` | `string` | Tool call ID |
| | `ToolName` | `string` | Tool name |
| | `Result` | `*ToolResult` | Execution result |
| | `Error` | `error` | Execution error |
| | `IsError` | `bool` | Error flag |

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewLoop` | `(client LLMClient) *Loop` | Creates new loop |
| `Run` | `(ctx context.Context, messages []AgentMessage, tools ...Tool) (*RunResult, error)` | Executes loop |
| `ExtractToolCalls` | `(msg AgentMessage) []ToolCallContent` | Extracts tool calls from message |
| `AddToolResults` | `(messages []AgentMessage, results []ToolExecutionResult) []AgentMessage` | Adds tool results to history |

## Error Handling

| Error Type | Handler | Behavior |
|------------|---------|----------|
| Context canceled | Returns `ctx.Err()` | Loop aborts |
| LLM stream error | Returns wrapped error | Loop fails |
| Tool not found | Creates error result | Continues with error result |
| Tool execution error | Creates error result | Continues with error result |

## Design Notes

### Sequential Tool Execution

**Q: Why execute tools sequentially instead of in parallel?**

| Aspect | Description |
|--------|-------------|
| Context | Tool call execution |
| Options considered | Sequential, parallel, limited parallelism |
| Rationale | Simpler error handling, predictable order |

**Current approach is recommended because:**
- Deterministic execution order
- Simpler error handling
- No goroutine management overhead
- Tools may have dependencies

### Continue on Tool Error

**Q: Why continue the loop when a tool fails instead of aborting?**

| Aspect | Description |
|--------|-------------|
| Context | Tool execution error handling |
| Options considered | Abort, continue with error, retry |
| Rationale | LLM can decide how to handle errors |

**Current approach is recommended because:**
- LLM sees error and can adapt
- Other tools still execute
- Agent can retry or try alternatives
- Standard pattern in agent systems

## Test Coverage

### Unit Tests (`loop_test.go`)

| Test Suite | Coverage |
|------------|----------|
| `TestNewLoop` | Loop creation |
| `TestRun_NoToolCalls` | Simple message flow |
| `TestRun_WithToolCalls` | Tool execution flow |
| `TestRun_ContextCanceled` | Context cancellation |
| `TestExtractToolCalls` | Tool call extraction |
| `TestAddToolResults` | Result message creation |

**Running:**
```bash
make test ./internal/agent
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `context` | Context handling |
| `fmt` | Error formatting |

**File Location:** [`internal/agent/loop.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/agent/loop.go)

## Related

- [Agent README](README.md) - Main agent package
- [Tool](tool.md) - Tool execution
- [Types](types.md) - Message types
