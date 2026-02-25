# Agent Loop

Tool calling execution loop. Handles LLM streaming, tool call extraction, and result collection.

## Overview

```mermaid
flowchart TB
    Start(["Run(ctx, messages, tools...)"]) --> SetTools["l.tools = tools"]
    SetTools --> CopyMsgs["make messages copy"]
    CopyMsgs --> Copy["copy messages"]
    Copy --> LoopStart{for loop}
    LoopStart --> SelectCtx["select"]
    SelectCtx -->|ctx.Done| ReturnCtxErr(["return nil, error"])
    SelectCtx -->|default| StreamCh["client.Stream()"]
    StreamCh -->|error| ReturnStreamErr(["return error"])
    StreamCh -->|ok| InitAssistant["assistantMsg init"]
    InitAssistant --> ForEvents["for event range"]
    ForEvents --> CheckEvent{event type}
    CheckEvent -->|MessageEndEvent| SetAssistant["set message"]
    CheckEvent -->|other| Ignore["ignore"]
    SetAssistant --> MoreEvents["continue"]
    Ignore --> MoreEvents
    MoreEvents --> ForEvents
    ForEvents -->|closed| CheckNilMsg{assistantMsg nil}
    CheckNilMsg -->|yes| BreakLoop["break"]
    CheckNilMsg -->|no| AppendMsg["append message"]
    AppendMsg --> ExtractCalls["extract tool calls"]
    ExtractCalls --> CheckCalls{no tool calls}
    CheckCalls -->|yes| BreakLoop2["break"]
    CheckCalls -->|no| ExecuteTools["execute tools"]
    ExecuteTools --> AddResults["add tool results"]
    AddResults --> LoopStart
    BreakLoop --> ReturnResult(["return result, nil"])
    BreakLoop2 --> ReturnResult

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnCtxErr,ReturnStreamErr,ReturnResult mainFn
    class ReturnCtxErr,ReturnStreamErr errorFlow
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
