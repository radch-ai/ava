# Agent Tool

Tool execution framework for the agent runtime.

## ExecuteToolCall

```mermaid
flowchart TB
    Start(["ExecuteToolCall(ctx, state, toolCall) → (AgentMessage, error)"]) --> FindTool["FindToolByName(state, toolCall.Name) → tool"]
    FindTool --> CheckTool{tool nil}
    CheckTool -->|yes| CreateErr1["&ToolExecutionError{ToolName: toolCall.Name, Message: 'tool not found'} → err"]
    CreateErr1 --> ErrorResult1["errorResult(toolCall, err) → errMsg"]
    ErrorResult1 --> Err1(["return errMsg, err"])
    CheckTool -->|no| CheckExecute{tool.Execute nil}
    CheckExecute -->|yes| CreateErr2["&ToolExecutionError{ToolName: toolCall.Name, Message: 'tool Execute function is nil'} → err"]
    CreateErr2 --> ErrorResult2["errorResult(toolCall, err) → errMsg"]
    ErrorResult2 --> Err2(["return errMsg, err"])
    CheckExecute -->|no| ExecuteTool["tool.Execute(ctx, toolCall.Arguments) → result, err"]
    ExecuteTool -->|error| CreateErr3["&ToolExecutionError{ToolName: toolCall.Name, Message: err.Error()} → toolErr"]
    CreateErr3 --> ErrorResult3["errorResult(toolCall, toolErr) → errMsg"]
    ErrorResult3 --> Err3(["return errMsg, toolErr"])
    ExecuteTool -->|ok| GetTime["time.Now().Unix() → ts"]
    GetTime --> CheckContent{result.Content empty}
    CheckContent -->|yes| SetEmpty["[]ContentBlock{TextContent{Text: ''}} → content"]
    SetEmpty --> ReturnMsg["AgentMessage{Role: RoleToolResult, Content: content, Timestamp: ts, ToolCallID: toolCall.ID, ToolName: toolCall.Name, IsError: false}"]
    CheckContent -->|no| SetContent["result.Content → content"]
    SetContent --> ReturnMsg
    ReturnMsg --> Ok(["return returnMsg, nil"])

    subgraph SGErrorResult [" "]
        HErrorResult1["**errorResult(toolCall, err) → AgentMessage**"]
        HErrorResult1 --> GetTime1["time.Now().Unix() → ts"]
        GetTime1 --> ReturnErr1(["return AgentMessage{Role: RoleToolResult, Content: []ContentBlock{TextContent{Text: err.Error()}}, Timestamp: ts, ToolCallID: toolCall.ID, ToolName: toolCall.Name, IsError: true}"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Ok,Err1,Err2,Err3 mainFn
    class FindTool localPkgs
    class HErrorResult1 internalFn
    class Err1,Err2,Err3 errorFlow
```

## FindToolByName

```mermaid
flowchart TB
    Start(["FindToolByName(state, name) → *Tool"]) --> ForTools["for i in state.Tools"]
    ForTools --> CheckName{"state.Tools[i].Name == name"}
    CheckName -->|yes| ReturnFound(["return &state.Tools[i]"])
    CheckName -->|no| LoopTools
    LoopTools --> ForTools
    ForTools -->|done| ReturnNil(["return nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,ReturnFound,ReturnNil mainFn
```

## Functions

| Function | Description |
|----------|-------------|
| `errorResult` | Internal - creates error result message |

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
