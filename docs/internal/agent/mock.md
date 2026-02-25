# Agent Mock

Mock implementations for testing the agent runtime.

## Stream

```mermaid
flowchart TB
    Start(["Stream(ctx, messages, tools) → (<-chan AgentEvent, error)"]) --> CheckLen{"m.Responses empty"}
    CheckLen -->|yes| MakeEmptyCh["make(chan AgentEvent) → ch"]
    MakeEmptyCh --> CloseEmpty["close(ch)"]
    CloseEmpty --> ReturnEmpty(["return ch, nil"])
    CheckLen -->|no| GetResp["m.Responses[m.responseIndex] → resp"]
    GetResp --> IncIndex["m.responseIndex = (m.responseIndex + 1) % len(m.Responses)"]
    IncIndex --> CheckBlock{"resp.BlockUntil nil"}
    CheckBlock -->|yes| Wait["<-resp.BlockUntil"]
    CheckBlock -->|no| MakeCh
    Wait --> MakeCh["make(chan AgentEvent, 10) → ch"]
    MakeCh --> Goroutine["go func()"]
    Goroutine --> DeferClose["defer close(ch)"]
    DeferClose --> SelectCtx["select{ctx.Done() default}"]
    SelectCtx -->|ctx.Done| ReturnFunc["return"]
    SelectCtx -->|default| GetTimestamp["time.Now().Unix() → timestamp"]
    GetTimestamp --> EmitStart["ch <- MessageStartEvent{Timestamp: timestamp, Message: resp.Message}"]
    EmitStart --> ExtractDelta{"resp.Message.Content[0] is TextContent"}
    ExtractDelta -->|yes| SetDelta["tc.Text → delta"]
    ExtractDelta -->|no| SetEmptyDelta["delta = ''"]
    SetDelta --> EmitUpdate
    SetEmptyDelta --> EmitUpdate["ch <- MessageUpdateEvent{Timestamp: timestamp, Message: resp.Message, Delta: delta}"]
    EmitUpdate --> EmitEnd["ch <- MessageEndEvent{Timestamp: timestamp, Message: resp.Message}"]
    EmitEnd --> ReturnFunc
    Goroutine --> Return(["return ch, nil"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,ReturnEmpty,Return mainFn
```

## Reset

```mermaid
flowchart TB
    Start(["Reset()"]) --> SetIndex["m.responseIndex = 0"]
    SetIndex --> Return(["return"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

## MockTool

```mermaid
flowchart TB
    Start(["MockTool(config) → agent.Tool"]) --> CheckName{"config.Name empty"}
    CheckName -->|yes| SetDefaultName["config.Name = 'mock_tool'"]
    CheckName -->|no| CheckExecuteFunc
    SetDefaultName --> CheckExecuteFunc{"config.ExecuteFunc nil"}
    CheckExecuteFunc -->|no| SetExecute["execute = config.ExecuteFunc"]
    CheckExecuteFunc -->|yes| CheckError{"config.Error nil"}
    CheckError -->|no| SetErrorFunc["execute = func(ctx, args) → return nil, config.Error"]
    CheckError -->|yes| CheckResult{"config.Result nil"}
    CheckResult -->|no| SetResultFunc["execute = func(ctx, args) → return config.Result, nil"]
    CheckResult -->|yes| SetDefaultFunc["execute = func(ctx, args) → return &agent.ToolResult{Content: []agent.ContentBlock{agent.TextContent{Text: 'ok'}}}, nil"]
    SetExecute --> ReturnTool
    SetErrorFunc --> ReturnTool
    SetResultFunc --> ReturnTool
    SetDefaultFunc --> ReturnTool(["return agent.Tool{Name: config.Name, Description: config.Description, Execute: execute}"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,ReturnTool mainFn
```

## MockEchoTool

```mermaid
flowchart TB
    Start(["MockEchoTool(name) → agent.Tool"]) --> ReturnTool(["return MockTool(MockToolConfig{Name: name, Description: 'Echoes back the input', ExecuteFunc: func})"])

    subgraph SGExecuteFunc [" "]
        HExecuteFunc["**ExecuteFunc(ctx, args) → (*agent.ToolResult, error)**"]
        HExecuteFunc --> GetInput["args['input'] → input, ok"]
        GetInput --> CheckInput{"ok false or input empty"}
        CheckInput -->|yes| ReturnErr(["return nil, &agent.ToolExecutionError{ToolName: name, Message: 'missing or invalid input argument'}"])
        CheckInput -->|no| ReturnOk(["return &agent.ToolResult{Content: []agent.ContentBlock{agent.TextContent{Text: input}}, Details: input}, nil"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,ReturnTool,ReturnOk mainFn
    class HExecuteFunc internalFn
    class ReturnErr errorFlow
```

## MockFailingTool

```mermaid
flowchart TB
    Start(["MockFailingTool(name, err) → agent.Tool"]) --> ReturnTool(["return MockTool(MockToolConfig{Name: name, Description: 'A tool that always fails', Error: err})"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,ReturnTool mainFn
```

## MockResultTool

```mermaid
flowchart TB
    Start(["MockResultTool(name, result) → agent.Tool"]) --> ReturnTool(["return MockTool(MockToolConfig{Name: name, Description: 'A tool with a canned result', Result: result})"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,ReturnTool mainFn
```

## Structs

### MockLLMClient

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `MockLLMClient` | `Responses` | `[]MockResponse` | Canned responses |
| | `responseIndex` | `int` | Current response index |

### MockResponse

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `MockResponse` | `Message` | `AgentMessage` | Response message |
| | `BlockUntil` | `chan struct{}` | Block until channel closed |

### MockToolConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `MockToolConfig` | `Name` | `string` | Tool name |
| | `Description` | `string` | Tool description |
| | `Result` | `*ToolResult` | Canned result |
| | `Error` | `error` | Canned error |
| | `ExecuteFunc` | `func(ctx context.Context, args map[string]any) (*ToolResult, error)` | Custom execution |

## Interface

### LLMClient

```go
type LLMClient interface {
    Stream(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan AgentEvent, error)
}
```

## Design Notes

### Response Cycling

**Q: Why cycle through responses instead of consuming them?**

| Aspect | Description |
|--------|-------------|
| Context | Mock response management |
| Options considered | Consume, cycle, infinite repeat |
| Rationale | Cycling allows reuse in multi-turn tests |

**Current approach is recommended because:**
- Tests can run multiple prompts
- Predictable response sequence
- Reset() available for explicit reset
- Avoids test setup complexity

### BlockUntil Channel

**Q: Why have BlockUntil instead of sleep/delay?**

| Aspect | Description |
|--------|-------------|
| Context | Testing async behavior |
| Options considered | time.Sleep, channel signal |
| Rationale | Channel signal is deterministic and instant |

**Current approach is recommended because:**
- Tests control exact timing
- No flaky sleep-based tests
- Can test concurrent scenarios
- Instant when unblocked

## Test Coverage

Used extensively in agent package tests. Not directly tested itself.

**Running:**
```bash
make test ./internal/agent
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `context` | Context handling |
| `time` | Timestamp generation |

**File Location:** [`internal/agent/mock.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/agent/mock.go)

## Related

- [Agent README](README.md) - Main agent package
- [Loop](loop.md) - Uses MockLLMClient
