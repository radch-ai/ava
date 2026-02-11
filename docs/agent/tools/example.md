# Example Tools

The tools subpackage provides example tools that demonstrate the agent tool interface. These tools can be used as templates for creating custom tools.

## Available Tools

### ExampleEchoTool

Returns a tool that echoes input text back to the caller.

```go
func ExampleEchoTool() agent.Tool
```

**Arguments:**
- `text` (string): The text to echo back

**Returns:** The same text that was provided

**Use case:** Testing the tool execution pipeline

#### Example

```go
tool := ExampleEchoTool()

result, err := tool.Execute(ctx, map[string]any{
    "text": "hello world",
})
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Content[0].(agent.TextContent).Text)
// Output: hello world
```

### ExampleMathTool

Performs simple arithmetic operations.

```go
func ExampleMathTool() agent.Tool
```

**Arguments:**
- `operation` (string): One of "add", "subtract", "multiply", "divide"
- `a` (float64): First operand
- `b` (float64): Second operand

**Returns:** Result formatted with 2 decimal places

**Operations:**
| Operation | Description | Example |
|-----------|--------------|----------|
| `add` | Returns a + b | 2 + 3 = 5 |
| `subtract` | Returns a - b | 5 - 3 = 2 |
| `multiply` | Returns a * b | 2 * 3 = 6 |
| `divide` | Returns a / b | 6 / 3 = 2 |

#### Example

```go
tool := ExampleMathTool()

result, err := tool.Execute(ctx, map[string]any{
    "operation": "add",
    "a": 2.0,
    "b": 3.0,
})
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Content[0].(agent.TextContent).Text)
// Output: 5.00

fmt.Println(result.Details)
// Output: 5
```

**Error handling:** Division by zero returns an error

```go
result, err := tool.Execute(ctx, map[string]any{
    "operation": "divide",
    "a": 5.0,
    "b": 0.0,
})
// err: "division by zero"
```

### ExampleTimeTool

Returns the current time in RFC3339 format.

```go
func ExampleTimeTool() agent.Tool
```

**Arguments:** None

**Returns:** Current timestamp in RFC3339 format

**Use case:** Demonstrates context cancellation support

#### Example

```go
tool := ExampleTimeTool()

result, err := tool.Execute(ctx, nil)
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Content[0].(agent.TextContent).Text)
// Output: 2025-02-11T12:34:56Z

fmt.Println(result.Details)
// Output: 2025-02-11T12:34:56Z
```

### ExampleDelayTool

Simulates long-running operations by waiting for a specified duration.

```go
func ExampleDelayTool() agent.Tool
```

**Arguments:**
- `duration_ms` (int or float64): Milliseconds to wait (must be non-negative)

**Returns:** Confirmation message with wait duration

**Use case:** Demonstrates proper context cancellation handling for long-running tools

#### Example

```go
tool := ExampleDelayTool()

result, err := tool.Execute(ctx, map[string]any{
    "duration_ms": 500,
})
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Content[0].(agent.TextContent).Text)
// Output: waited 500 ms
```

**Context cancellation:** The tool properly handles cancellation:

```go
ctx, cancel := context.WithCancel(context.Background())

go func() {
    time.Sleep(100 * time.Millisecond)
    cancel() // Cancel after 100ms
}()

result, err := tool.Execute(ctx, map[string]any{
    "duration_ms": 5000, // 5 second delay
})
// err: context canceled
```

**Negative duration validation:**

```go
result, err := tool.Execute(ctx, map[string]any{
    "duration_ms": -100,
})
// err: "duration_ms must be non-negative"
```

## Implementation Patterns

### Argument Validation

All example tools demonstrate proper argument validation:

```go
// Type assertion with check
value, ok := args["value"].(string)
if !ok {
    return nil, fmt.Errorf("missing or invalid 'value' argument")
}

// Empty value check
if value == "" {
    return nil, fmt.Errorf("missing or invalid 'value' argument")
}
```

### Flexible Type Handling

ExampleDelayTool accepts both int and float64 for duration:

```go
durationMsFloat, ok := args["duration_ms"].(float64)
if !ok {
    // Also accept int values for convenience
    durationMsInt, okInt := args["duration_ms"].(int)
    if !okInt {
        return nil, fmt.Errorf("missing or invalid 'duration_ms' argument")
    }
    durationMsFloat = float64(durationMsInt)
}
```

### Structured Results

ExampleMathTool returns both formatted text and structured data:

```go
return &agent.ToolResult{
    Content: []agent.ContentBlock{agent.TextContent{Text: fmt.Sprintf("%.2f", result)}},
    Details: result, // Raw numeric value for programmatic access
}, nil
```

## Using Example Tools with Agent

```go
import "github.com/nevaria-ai/gateway/internal/agent/tools"

// Create agent
agent := NewAgent(AgentOptions{
    SystemPrompt: "You are a helpful assistant with access to tools.",
    SessionID:    "session-123",
})

// Configure tools
state := agent.State()
state.Tools = []agent.Tool{
    tools.ExampleEchoTool(),
    tools.ExampleMathTool(),
    tools.ExampleTimeTool(),
    tools.ExampleDelayTool(),
}

// Process message
err := agent.Prompt(ctx, "What's 2 + 2?")
if err != nil {
    log.Fatal(err)
}
```

## See Also

- [../types.md](../types.md) - Tool type definition
- [../tool.go.md](../tool.go.md) - Tool execution
- [registry.md](registry.md) - Tool registry
