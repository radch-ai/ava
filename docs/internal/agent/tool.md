# Agent Tool

Tool execution framework for the agent runtime.

## Overview

```mermaid
flowchart TB
    Start(["ExecuteToolCall(ctx, state, toolCall)"]) --> HFind

    subgraph SGExecute [" "]
        HFind["**FindToolByName() → *Tool**"]
        HFind -->|nil| ErrNotFound["return ToolExecutionError"]
        HFind -->|found| CheckExecute{Execute != nil?}
        CheckExecute -->|no| ErrNilExecute["return ToolExecutionError"]
        CheckExecute -->|yes| DoExecute["tool.Execute(ctx, args)"]
        DoExecute -->|error| ErrExecute["return error result"]
        DoExecute -->|ok| BuildResult["build AgentMessage"]
    end

    ErrNotFound --> ErrReturn(["return error"])
    ErrNilExecute --> ErrReturn
    ErrExecute --> ErrMsgReturn(["return error message"])
    BuildResult --> Success(["return result message"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Success,ErrMsgReturn mainFn
    class HFind,DoExecute,BuildResult internalFn
    class ErrNotFound,ErrNilExecute,ErrExecute,ErrReturn errorFlow
```

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExecuteToolCall` | `(ctx context.Context, state *AgentState, toolCall ToolCallContent) (AgentMessage, error)` | Executes a tool call |
| `FindToolByName` | `(state *AgentState, name string) *Tool` | Finds tool by name |

## Error Handling

| Error Type | Handler | Behavior |
|------------|---------|----------|
| Tool not found | Returns error message | `IsError: true` in result |
| Nil Execute function | Returns error message | `IsError: true` in result |
| Tool execution error | Returns error message | `IsError: true` in result |

## Design Notes

### Error as Tool Result

**Q: Why return tool execution errors as messages instead of errors?**

| Aspect | Description |
|--------|-------------|
| Context | Tool error handling |
| Options considered | Return error, return error message, both |
| Rationale | LLM needs to see errors in conversation |

**Current approach is recommended because:**
- LLM can see error and adapt
- Error becomes part of conversation
- Agent can retry or try alternatives
- Consistent with OpenAI/Anthropic patterns

### Pointer Return for FindToolByName

**Q: Why return *Tool instead of Tool?**

| Aspect | Description |
|--------|-------------|
| Context | Tool lookup |
| Options considered | Tool with ok bool, *Tool, index |
| Rationale | Nil clearly indicates not found |

**Current approach is recommended because:**
- Nil is clear "not found" signal
- Avoids extra bool return
- Pointer allows caller to check fields
- Standard Go pattern for lookups

## Test Coverage

### Unit Tests (`tool_test.go`)

| Test Suite | Coverage |
|------------|----------|
| `TestExecuteToolCall_Success` | Successful execution |
| `TestExecuteToolCall_NotFound` | Tool not found |
| `TestExecuteToolCall_NilExecute` | Nil Execute function |
| `TestExecuteToolCall_Error` | Tool returns error |
| `TestFindToolByName` | Tool lookup |
| `TestFindToolByName_NotFound` | Not found returns nil |

**Running:**
```bash
make test ./internal/agent
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `context` | Context handling |
| `fmt` | Error formatting |
| `time` | Timestamp generation |

**File Location:** [`internal/agent/tool.go`](../../internal/agent/tool.go)

## Related

- [Agent README](README.md) - Main agent package
- [Loop](loop.md) - Tool calling loop
- [Types](types.md) - Tool and ToolResult types
