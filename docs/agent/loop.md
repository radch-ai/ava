# Loop

The Loop type manages the agent execution loop, handling LLM streaming, tool call extraction, and tool result aggregation. This document covers the Loop struct and its methods.

## Loop Struct

```go
type Loop struct {
    client LLMClient
    tools  []Tool
}
```

### Fields

| Field | Description |
|-------|-------------|
| `client` | LLM client for streaming responses |
| `tools` | Available tools for the agent |

## Creating a Loop

### NewLoop

Creates a new Loop with the given LLM client.

```go
func NewLoop(client LLMClient) *Loop
```

#### Example

```go
client := NewClient(config, model)
loop := NewLoop(client)
```

## Running the Loop

### Run

Executes the agent loop starting with the given messages. Returns when the LLM responds without tool calls or on error.

```go
type RunResult struct {
    Messages []AgentMessage
}

func (l *Loop) Run(ctx context.Context, messages []AgentMessage, tools ...Tool) (*RunResult, error)
```

### Execution Flow

1. Store tools for use during execution
2. Create a copy of the initial messages
3. Loop until no tool calls are returned:
   - Check context cancellation
   - Stream from LLM with current messages and tools
   - Collect the final MessageEndEvent
   - Add assistant message to history
   - Extract tool calls from the message
   - If no tool calls, exit loop
   - Execute all tools
   - Add tool results to history

#### Example

```go
messages := []AgentMessage{
    NewTextMessage(RoleUser, "What's 2 + 2?"),
}

result, err := loop.Run(ctx, messages, ExampleMathTool())
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Final messages: %d\n", len(result.Messages))
```

## Tool Call Extraction

### ExtractToolCalls

Extracts tool calls from an assistant message.

```go
func (l *Loop) ExtractToolCalls(msg AgentMessage) []ToolCallContent
```

Only processes messages with `RoleAssistant`. Any `ToolCallContent` blocks in the message content are extracted and returned.

#### Return Value

Slice of `ToolCallContent` structs, or empty slice if:
- Message role is not `RoleAssistant`
- Message contains no tool call content blocks

#### Example

```go
msg := NewToolCallMessage("call-123", "math", map[string]any{
    "operation": "add",
    "a": 2.0,
    "b": 2.0,
})

toolCalls := loop.ExtractToolCalls(msg)
// toolCalls[0].Name == "math"
```

## Adding Tool Results

### AddToolResults

Adds tool result messages to the message history.

```go
func (l *Loop) AddToolResults(messages []AgentMessage, results []ToolExecutionResult) []AgentMessage
```

Creates a new message list with tool result messages appended. For each result:
- If error: creates an error result message with `IsError: true`
- If result has content: creates a result message with text content
- If empty result: creates an empty result message

#### Example

```go
results := []ToolExecutionResult{
    {
        ToolCallID: "call-123",
        ToolName:   "math",
        Result:     &ToolResult{Content: []ContentBlock{TextContent{Text: "4"}}},
        IsError:    false,
    },
}

updated := loop.AddToolResults(messages, results)
```

## Internal Types

### ToolExecutionResult

Represents the result of executing a tool.

```go
type ToolExecutionResult struct {
    ToolCallID string
    ToolName   string
    Result     *ToolResult
    Error      error
    IsError    bool
}
```

| Field | Description |
|-------|-------------|
| `ToolCallID` | Tool call identifier from the request |
| `ToolName` | Name of the executed tool |
| `Result` | Tool result (nil if error) |
| `Error` | Error from tool execution (nil if success) |
| `IsError` | True if execution failed |

## Tool Execution

### executeTools

Internal method that executes tool calls and returns their results.

```go
func (l *Loop) executeTools(ctx context.Context, toolCalls []ToolCallContent) []ToolExecutionResult
```

For each tool call:
1. Find tool by name in the registered tools
2. If not found, return error result
3. If tool Execute is nil, return error result
4. Execute tool with arguments
5. Return result or error

#### Error Handling

Errors are returned as `ToolExecutionResult` with `IsError: true`:
- Tool not found: `"tool \"name\" not found"`
- Execute is nil: `"tool Execute function is nil"`
- Execution error: wraps the original error

## LLM Client Interface

The Loop requires an LLM client that implements this interface:

```go
type LLMClient interface {
    Stream(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan AgentEvent, error)
}
```

See [llm.md](../llm.md) for LLM client implementations.

## Complete Example

```go
// Create LLM client
client := NewClient(config, model)

// Create loop
loop := NewLoop(client)

// Start with user message
messages := []AgentMessage{
    NewTextMessage(RoleUser, "What's the time?"),
}

// Run with time tool
result, err := loop.Run(ctx, messages, ExampleTimeTool())
if err != nil {
    log.Fatal(err)
}

// Process final messages
for _, msg := range result.Messages {
    switch msg.Role {
    case RoleUser:
        fmt.Printf("User: %s\n", msg.Content[0].(TextContent).Text)
    case RoleAssistant:
        fmt.Printf("Assistant: %s\n", msg.Content[0].(TextContent).Text)
    case RoleToolResult:
        fmt.Printf("Tool Result: %s\n", msg.Content[0].(TextContent).Text)
    }
}
```

## See Also

- [agent.md](agent.md) - Agent struct with Prompt method
- [tool.go.md](tool.go.md) - Tool execution functions
- [events.md](events.md) - Event types for streaming
- [types.md](types.md) - Core type definitions
