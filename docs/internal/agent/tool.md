# Agent Tool

Tool execution framework for the agent runtime.

## Overview

```mermaid
flowchart TB
    Start(["ExecuteToolCall(ctx, state, toolCall)"]) --> FindTool["FindToolByName()"]
    FindTool -->|nil| CreateErr1["create not found error"]
    FindTool -->|found| CheckExecute{Execute nil}
    CheckExecute -->|yes| CreateErr2["create nil error"]
    CheckExecute -->|no| CallExecute["tool.Execute()"]
    CreateErr1 --> ErrorResult1["error result"]
    CreateErr2 --> ErrorResult2["error result"]
    CallExecute -->|error| CreateToolErr["create tool error"]
    CallExecute -->|ok| CheckContent{content empty}
    CreateToolErr --> ErrorResult3["error result"]
    CheckContent -->|yes| SetEmpty["set empty content"]
    CheckContent -->|no| SetContent["set result content"]
    SetEmpty --> ReturnOk(["return success message"])
    SetContent --> ReturnOk
    ErrorResult1 --> ReturnErr1(["return error"])
    ErrorResult2 --> ReturnErr2
    ErrorResult3 --> ReturnErr3

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnOk,ReturnErr1,ReturnErr2,ReturnErr3 mainFn
    class ReturnErr1,ReturnErr2,ReturnErr3 errorFlow
```

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExecuteToolCall` | `(ctx context.Context, state *AgentState, toolCall ToolCallContent) (AgentMessage, error)` | Executes a tool call |
| `FindToolByName` | `(state *AgentState, name string) *Tool` | Finds tool by name |

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

**File Location:** [`internal/agent/tool.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/agent/tool.go)

## Related

- [Agent README](README.md) - Main agent package
- [Loop](loop.md) - Tool calling loop
- [Types](types.md) - Tool and ToolResult types
