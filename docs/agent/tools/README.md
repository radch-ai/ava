# Tools Subpackage

The tools subpackage provides example tools demonstrating the agent tool interface and a thread-safe registry for tool management.

## Overview

- **Example Tools**: Ready-to-use tools for testing and development
- **Registry**: Thread-safe tool registration and lookup
- **Test Patterns**: Templates for creating custom tools

## Documentation

| Document | Description |
|----------|-------------|
| [example.md](example.md) | ExampleEchoTool, ExampleMathTool, ExampleTimeTool, ExampleDelayTool |
| [registry.md](registry.md) | Registry type, RegisterTool, GetTool, DefaultTools |

## Quick Start

```go
import "github.com/nevaria-ai/gateway/internal/agent/tools"

// Create a registry with default tools
registry := NewRegistry()
for _, tool := range DefaultTools() {
    RegisterTool(registry, tool.Name, tool)
}

// Add a custom tool
customTool := agent.Tool{
    Name:        "my_tool",
    Description: "Does something useful",
    Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
        return &agent.ToolResult{
            Content: []agent.ContentBlock{agent.TextContent{Text: "result"}},
        }, nil
    },
}
RegisterTool(registry, customTool.Name, customTool)

// Use with agent
state := agent.State()
state.Tools = []agent.Tool{
    ExampleEchoTool(),
    ExampleMathTool(),
}
```

## Tool Design Patterns

### Argument Parsing

All arguments are passed as `map[string]any`. Always validate types:

```go
func ExampleTool() agent.Tool {
    return agent.Tool{
        Name: "example",
        Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
            // Validate required argument
            text, ok := args["text"].(string)
            if !ok || text == "" {
                return nil, fmt.Errorf("missing or invalid 'text' argument")
            }

            // Process with validated argument
            return &agent.ToolResult{
                Content: []agent.ContentBlock{agent.TextContent{Text: text}},
            }, nil
        },
    }
}
```

### Error Handling

Return descriptive errors for invalid inputs:

```go
if value, ok := args["value"].(float64); !ok {
    return nil, fmt.Errorf("missing or invalid 'value' argument: expected number")
}
```

### Context Cancellation

Always check context cancellation in long-running tools:

```go
func LongRunningTool() agent.Tool {
    return agent.Tool{
        Name: "long_running",
        Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
            // Check cancellation before starting
            select {
            case <-ctx.Done():
                return nil, ctx.Err()
            default:
            }

            // Do work...

            // Check during work
            select {
            case <-ctx.Done():
                return nil, ctx.Err()
            default:
            }

            return result, nil
        },
    }
}
```

## See Also

- [example.md](example.md) - Complete example tool implementations
- [registry.md](registry.md) - Tool registry documentation
- [../tool.go.md](../tool.go.md) - Tool execution framework
