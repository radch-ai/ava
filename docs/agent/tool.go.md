# Tool Execution

The tool execution framework provides functions for executing tool calls and finding tools by name. This document covers `tool.go` functions.

## ExecuteToolCall

Executes a tool call from an agent. Looks up the tool by name, calls its Execute function with context, and returns a result message.

```go
func ExecuteToolCall(ctx context.Context, state *AgentState, toolCall ToolCallContent) (AgentMessage, error)
```

### Execution Flow

1. Find tool by name using `FindToolByName`
2. If not found, return error result with `ToolExecutionError`
3. If tool Execute is nil, return error result
4. Call tool's Execute function with context and arguments
5. If execution fails, return error result
6. Build result message from tool result content

### Return Value

Returns a tuple of:
- `AgentMessage`: Message with role `RoleToolResult`
- `error`: The execution error (nil on success)

### Example

```go
state := agent.State()
toolCall := ToolCallContent{
    ID:   "call-123",
    Name: "echo",
    Arguments: map[string]any{"text": "hello"},
}

msg, err := ExecuteToolCall(ctx, &state, toolCall)
if err != nil {
    log.Printf("Tool execution failed: %v", err)
}

// msg.Role == RoleToolResult
// msg.ToolCallID == "call-123"
// msg.IsError == true (if err != nil)
```

## FindToolByName

Locates a tool in the agent's tool list by name.

```go
func FindToolByName(state *AgentState, name string) *Tool
```

### Return Value

- Pointer to the tool if found
- `nil` if tool is not found

### Example

```go
state := agent.State()

tool := FindToolByName(&state, "echo")
if tool == nil {
    log.Printf("Tool not found")
    return
}

fmt.Printf("Tool: %s - %s\n", tool.Name, tool.Description)
```

## Error Handling

### ToolExecutionError

Error type for tool execution failures.

```go
type ToolExecutionError struct {
    ToolName string
    Message  string
}
```

### Error Messages

| Condition | Error Message |
|------------|---------------|
| Tool not found | `tool "name" not found` |
| Execute is nil | `tool Execute function is nil` |
| Execution error | Wraps the original error message |

### Error Method

```go
func (e *ToolExecutionError) Error() string
```

Returns formatted error string: `"tool name: message"`

## Error Result Message

The `errorResult` internal function creates an error result message:

```go
func errorResult(toolCall ToolCallContent, err error) AgentMessage
```

Creates a message with:
- Role: `RoleToolResult`
- Content: Error text as `TextContent`
- ToolCallID: From the tool call
- ToolName: From the tool call
- IsError: `true`

## Complete Example

```go
func handleToolCall(agent *Agent, toolCall ToolCallContent) error {
    state := agent.State()

    msg, err := ExecuteToolCall(context.Background(), &state, toolCall)
    if err != nil {
        // Check if it's a tool execution error
        if toolErr, ok := err.(*ToolExecutionError); ok {
            log.Printf("Tool %s failed: %s", toolErr.ToolName, toolErr.Message)
        }
        return err
    }

    // Add result to conversation
    state.Messages = append(state.Messages, msg)

    // Emit event (if using event system)
    // agent.emit(ToolExecutionEndEvent{
    //     Timestamp:  msg.Timestamp,
    //     ToolCallID: msg.ToolCallID,
    //     ToolName:   msg.ToolName,
    //     Result:     msg.Content[0].(TextContent).Text,
    //     IsError:    msg.IsError,
    // })

    return nil
}
```

## Tool Execution Best Practices

### 1. Context Handling

Always respect context cancellation in tool implementations:

```go
func MyTool() Tool {
    return Tool{
        Name: "my_tool",
        Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
            select {
            case <-ctx.Done():
                return nil, ctx.Err()
            default:
            }
            // Tool logic here
        },
    }
}
```

### 2. Input Validation

Validate all arguments with clear error messages:

```go
func ValidatedTool() Tool {
    return Tool{
        Name: "validated_tool",
        Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
            value, ok := args["required"].(string)
            if !ok || value == "" {
                return nil, &ToolExecutionError{
                    ToolName: "validated_tool",
                    Message:  "missing or invalid 'required' argument",
                }
            }
            // Tool logic here
        },
    }
}
```

### 3. Structured Results

Use the Details field for programmatic access:

```go
return &ToolResult{
    Content: []ContentBlock{TextContent{Text: "Temperature: 72F"}},
    Details: map[string]any{
        "temperature": 72,
        "unit":        "F",
        "location":    location,
    },
}, nil
```

## See Also

- [types.md](types.md) - Tool and ToolResult definitions
- [tools/](tools/) - Example tool implementations
- [loop.md](loop.md) - Tool execution in the loop
