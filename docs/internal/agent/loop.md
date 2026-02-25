# Agent Loop

Tool calling execution loop. Handles LLM streaming, tool call extraction, and result collection.

## NewLoop

```mermaid
flowchart TB
    Start(["NewLoop(client) → *Loop"]) --> Return(["return &Loop{client: client}"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## Run

```mermaid
flowchart TB
    Start(["Run(ctx, messages, tools...) → (*RunResult, error)"]) --> SetTools["l.tools = tools"]
    SetTools --> CopyMsgs["make([]AgentMessage, len(messages)) → currentMessages"]
    CopyMsgs --> CopyMsgsData["copy(currentMessages, messages)"]
    CopyMsgsData --> LoopStart{for}
    LoopStart --> CheckCtx["select{ctx.Done() default}"]
    CheckCtx -->|ctx.Done| ErrCtx(["return nil, ctx.Err()"])
    CheckCtx -->|default| StreamCh["l.client.Stream(ctx, currentMessages, l.tools) → eventCh, err"]
    StreamCh -->|error| ErrStream(["return nil, error"])
    StreamCh -->|ok| InitAssistant["var assistantMsg *AgentMessage"]
    InitAssistant --> ForEvents["for event := range eventCh"]
    ForEvents --> SwitchEvent{event type}
    SwitchEvent -->|MessageEndEvent| SetAssistant["assistantMsg = &e.Message"]
    SwitchEvent -->|other| IgnoreEvent["ignore event"]
    SetAssistant --> LoopEvents
    IgnoreEvent --> LoopEvents["continue"]
    LoopEvents --> ForEvents
    ForEvents -->|closed| CheckNil{assistantMsg nil}
    CheckNil -->|yes| BreakLoop["break"]
    CheckNil -->|no| AppendMsg["currentMessages = append(currentMessages, *assistantMsg)"]
    AppendMsg --> ExtractCalls["l.ExtractToolCalls(*assistantMsg) → toolCalls"]
    ExtractCalls --> CheckCalls{no tool calls}
    CheckCalls -->|yes| BreakLoop2["break"]
    CheckCalls -->|no| ExecuteTools["l.executeTools(ctx, toolCalls) → results"]
    ExecuteTools --> AddResults["l.AddToolResults(currentMessages, results) → currentMessages"]
    AddResults --> LoopStart
    BreakLoop --> Return(["return &RunResult{Messages: currentMessages}, nil"])
    BreakLoop2 --> Return

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class ExtractCalls,AddResults localPkgs
    class ErrCtx,ErrStream errorFlow
```

## ExtractToolCalls

```mermaid
flowchart TB
    Start(["ExtractToolCalls(msg) → []ToolCallContent"]) --> CheckRole{"msg.Role != RoleAssistant"}
    CheckRole -->|yes| ReturnNil(["return nil"])
    CheckRole -->|no| MakeCalls["make([]ToolCallContent, 0) → calls"]
    MakeCalls --> ForBlocks["for block in msg.Content"]
    ForBlocks --> TypeAssert{"block is ToolCallContent"}
    TypeAssert -->|yes| AppendCalls["calls = append(calls, tc)"]
    TypeAssert -->|no| LoopBlocks
    AppendCalls --> LoopBlocks["continue"]
    LoopBlocks --> ForBlocks
    ForBlocks -->|done| Return(["return calls"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,ReturnNil,Return mainFn
```

## AddToolResults

```mermaid
flowchart TB
    Start(["AddToolResults(messages, results) → []AgentMessage"]) --> CopyMsgs["make([]AgentMessage, len(messages)) → updated"]
    CopyMsgs --> CopyMsgsData["copy(updated, messages)"]
    CopyMsgsData --> ForResults["for result in results"]
    ForResults --> CheckError{"result.IsError"}
    CheckError -->|yes| ErrText["result.Error.Error() → text"]
    ErrText --> NewErrMsg["NewToolResultMessage(result.ToolCallID, result.ToolName, text, true) → resultMsg"]
    NewErrMsg --> AppendUpdated
    CheckError -->|no| CheckResult{"result.Result nil or empty"}
    CheckResult -->|yes| EmptyResultMsg["NewToolResultMessage(result.ToolCallID, result.ToolName, '', false) → resultMsg"]
    EmptyResultMsg --> AppendUpdated["updated = append(updated, resultMsg)"]
    CheckResult -->|no| TypeAssertContent{"result.Result.Content[0] is TextContent"}
    TypeAssertContent -->|yes| ExtractText["tc.Text → text"]
    ExtractText --> SuccessMsg["NewToolResultMessage(result.ToolCallID, result.ToolName, text, false) → resultMsg"]
    SuccessMsg --> AppendUpdated
    TypeAssertContent -->|no| AppendUpdated
    AppendUpdated --> LoopResults
    LoopResults --> ForResults
    ForResults -->|done| Return(["return updated"])

    click NewToolResultMessage href "types.md"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Return mainFn
    class NewToolResultMessage localPkgs
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

| Function | Description |
|----------|-------------|
| `executeTools` | Internal - executes tool calls |

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
