# Agent Core API Reference

## Overview

The agent core provides a comprehensive framework for building AI agents with tool execution, state management, and event broadcasting capabilities. The framework is designed for thread-safe operations, concurrent event subscriptions, and context-aware prompt execution with tool calling.

## Package: `internal/agent`

### Package Overview

The `internal/agent` package provides core types and interfaces for the agent runtime framework. It includes:

- Message types with support for text, image, and tool call content
- Tool execution framework with error handling
- State management with thread-safe operations
- Event broadcasting system for real-time updates
- Agent loop for managing tool calling interactions

### Core Types

#### `Role`

`Role` represents the role of a message sender.

```go
type Role string

const (
    RoleUser       Role = "user"
    RoleAssistant  Role = "assistant"
    RoleToolResult Role = "toolResult"
)
```

**Methods:**
- `String() string` - Returns the string representation of the role

#### `ContentBlock`

`ContentBlock` is a sealed interface for content blocks in messages. All content block types implement this interface.

**Types implementing ContentBlock:**
- `TextContent`
- `ImageContent`
- `ToolCallContent`

#### `TextContent`

`TextContent` represents a text block in a message.

```go
type TextContent struct {
    Text string `json:"text"`
}
```

#### `ImageContent`

`ImageContent` represents an image block in a message with base64-encoded data.

```go
type ImageContent struct {
    Data     string `json:"data"`
    MimeType string `json:"mime_type"`
}
```

#### `ToolCallContent`

`ToolCallContent` represents a tool invocation request from an assistant.

```go
type ToolCallContent struct {
    ID        string         `json:"id"`
    Name      string         `json:"name"`
    Arguments map[string]any `json:"arguments"`
}
```

#### `AgentMessage`

`AgentMessage` represents a message in the agent conversation.

```go
type AgentMessage struct {
    Role       Role           `json:"role"`
    Content    []ContentBlock `json:"content"`
    Timestamp  int64          `json:"timestamp"`
    ToolCallID string         `json:"tool_call_id,omitempty"`
    ToolName   string         `json:"tool_name,omitempty"`
    IsError    bool           `json:"is_error,omitempty"`
}
```

**Constructor Functions:**
- `NewTextMessage(role Role, text string) AgentMessage` - Creates a new text-only message
- `NewToolCallMessage(id, name string, args map[string]any) AgentMessage` - Creates a new message with a tool call from an assistant
- `NewToolResultMessage(toolCallID, toolName, result string, isError bool) AgentMessage` - Creates a new tool result message

#### `ThinkingLevel`

`ThinkingLevel` represents the thinking/reasoning intensity level.

```go
type ThinkingLevel string

const (
    ThinkingLevelOff     ThinkingLevel = "off"
    ThinkingLevelMinimal ThinkingLevel = "minimal"
    ThinkingLevelLow     ThinkingLevel = "low"
    ThinkingLevelMedium  ThinkingLevel = "medium"
    ThinkingLevelHigh    ThinkingLevel = "high"
)
```

**Methods:**
- `String() string` - Returns the string representation of the thinking level
- `IsOff() bool` - Returns true if thinking is disabled
- `IsEnabled() bool` - Returns true if thinking is enabled (any non-off level)

#### `Model`

`Model` represents an LLM model configuration.

```go
type Model struct {
    ID       string
    API      string
    Provider string
    BaseURL  string
}
```

#### `Tool`

`Tool` represents a callable tool function.

```go
type Tool struct {
    Name        string
    Description string
    Execute     func(ctx context.Context, args map[string]any) (*ToolResult, error)
}
```

#### `ToolResult`

`ToolResult` represents the result of a tool execution.

```go
type ToolResult struct {
    Content []ContentBlock
    Details any
}
```

#### `ToolExecutionError`

`ToolExecutionError` represents an error that occurred during tool execution.

```go
type ToolExecutionError struct {
    ToolName string
    Message  string
}
```

**Methods:**
- `Error() string` - Returns the error message with tool name prefix if available

#### `AgentStateOptions`

`AgentStateOptions` configures a new AgentState.

```go
type AgentStateOptions struct {
    SystemPrompt  string
    Model         Model
    ThinkingLevel ThinkingLevel
    Tools         []Tool
    SessionID     string
}
```

#### `AgentState`

`AgentState` tracks the current state of an agent session.

```go
type AgentState struct {
    SystemPrompt     string
    Model            Model
    ThinkingLevel    ThinkingLevel
    Tools            []Tool
    Messages         []AgentMessage
    IsStreaming      bool
    StreamMessage    *AgentMessage
    PendingToolCalls map[string]bool
    Error            string
    SessionID        string
}
```

**Constructor and Methods:**
- `NewAgentState(opts AgentStateOptions) AgentState` - Creates a new AgentState with the given options
- `AddMessage(msg AgentMessage)` - Appends a message to the conversation history
- `SetStreaming(streaming bool, msg *AgentMessage)` - Updates the streaming state and optionally the current stream message
- `AddPendingToolCall(id string)` - Marks a tool call as pending
- `RemovePendingToolCall(id string)` - Removes a tool call from the pending set
- `HasPendingToolCall(id string) bool` - Returns true if the tool call is still pending

### Agent Package

#### `AgentOptions`

`AgentOptions` configures a new Agent.

```go
type AgentOptions struct {
    ID           string
    SystemPrompt string
    SessionID    string
}
```

#### `Agent`

`Agent` manages agent state with thread-safe access and event broadcasting.

```go
type Agent struct {
    mu           sync.RWMutex
    state        AgentState
    id           string
    subscribers  map[string]chan AgentEvent
    subscribersMu sync.Mutex
    ctx          context.Context
    cancel       context.CancelFunc
}
```

**Constructor and Methods:**
- `NewAgent(opts AgentOptions) *Agent` - Creates a new Agent with the given options
- `Subscribe() (string, chan AgentEvent)` - Registers a new event channel and returns its subscription ID
- `Unsubscribe(id string)` - Removes the event channel for the given subscription ID
- `State() AgentState` - Returns a copy of the current agent state
- `Prompt(ctx context.Context, message string) error` - Sends a user message to the agent and runs the tool calling loop

### Events Package

#### `AgentEvent`

`AgentEvent` is a marker interface for all agent event types.

```go
type AgentEvent interface {
    isAgentEvent()
}
```

#### Event Types

1. **AgentStartEvent** - Emitted when an agent starts processing
    ```go
    type AgentStartEvent struct {
        Timestamp int64
    }
    ```

2. **AgentEndEvent** - Emitted when an agent finishes processing
    ```go
    type AgentEndEvent struct {
        Timestamp int64
        Messages  []AgentMessage
    }
    ```

3. **TurnStartEvent** - Emitted when a new turn (interaction cycle) begins
    ```go
    type TurnStartEvent struct {
        Timestamp int64
    }
    ```

4. **TurnEndEvent** - Emitted when a turn completes
    ```go
    type TurnEndEvent struct {
        Timestamp   int64
        Message     AgentMessage
        ToolResults []AgentMessage
    }
    ```

5. **MessageStartEvent** - Emitted when a message starts being generated
    ```go
    type MessageStartEvent struct {
        Timestamp int64
        Message   AgentMessage
    }
    ```

6. **MessageUpdateEvent** - Emitted when streaming content is added to a message
    ```go
    type MessageUpdateEvent struct {
        Timestamp int64
        Message   AgentMessage
        Delta     string
    }
    ```

7. **MessageEndEvent** - Emitted when a message is complete
    ```go
    type MessageEndEvent struct {
        Timestamp int64
        Message   AgentMessage
    }
    ```

8. **ToolExecutionStartEvent** - Emitted when a tool execution begins
    ```go
    type ToolExecutionStartEvent struct {
        Timestamp  int64
        ToolCallID string
        ToolName   string
        Args       map[string]any
    }
    ```

9. **ToolExecutionUpdateEvent** - Emitted when a tool produces partial results during execution
    ```go
    type ToolExecutionUpdateEvent struct {
        Timestamp     int64
        ToolCallID    string
        ToolName      string
        PartialResult any
    }
    ```

10. **ToolExecutionEndEvent** - Emitted when a tool execution completes
    ```go
    type ToolExecutionEndEvent struct {
        Timestamp  int64
        ToolCallID string
        ToolName   string
        Result     any
        IsError    bool
    }
    ```

### Loop Package

#### `LLMClient`

`LLMClient` defines the interface for calling LLM APIs.

```go
type LLMClient interface {
    Stream(ctx context.Context, messages []AgentMessage, tools []Tool) (<-chan AgentEvent, error)
}
```

#### `ToolExecutionResult`

`ToolExecutionResult` represents the result of executing a tool.

```go
type ToolExecutionResult struct {
    ToolCallID string
    ToolName   string
    Result     *ToolResult
    Error      error
    IsError    bool
}
```

#### `Loop`

`Loop` manages the agent execution loop.

```go
type Loop struct {
    client LLMClient
    tools  []Tool
}
```

**Constructor and Methods:**
- `NewLoop(client LLMClient) *Loop` - Creates a new Loop with the given LLM client
- `Run(ctx context.Context, messages []AgentMessage, tools ...Tool) (*RunResult, error)` - Executes the agent loop starting with the given messages
- `ExtractToolCalls(msg AgentMessage) []ToolCallContent` - Extracts tool calls from an assistant message
- `AddToolResults(messages []AgentMessage, results []ToolExecutionResult) []AgentMessage` - Adds tool result messages to the message history

#### `RunResult`

`RunResult` is the result of running the loop.

```go
type RunResult struct {
    Messages []AgentMessage
}
```

### Tool Package

#### Utility Functions

**ExecuteToolCall**
```go
func ExecuteToolCall(ctx context.Context, state *AgentState, toolCall ToolCallContent) (AgentMessage, error)
```
Executes a tool call from the agent. It looks up the tool by name in the agent's state, calls the tool's Execute function with context, and returns a ToolResult as an AgentMessage with RoleToolResult.

**FindToolByName**
```go
func FindToolByName(state *AgentState, name string) *Tool
```
Locates a tool in the agent's tool list by name. Returns nil if the tool is not found.

#### Error Handling

The framework provides specialized error types for better error reporting:

```go
// Example tool execution error
err := &ToolExecutionError{
    ToolName: "calculator",
    Message:  "division by zero",
}
fmt.Println(err.Error()) // "tool calculator: division by zero"
```

## Package: `internal/agent/tools`

### Package Overview

The `tools` package provides example tools demonstrating the agent tool interface. These tools can be used as templates for creating custom tools.

### Example Tools

#### `ExampleEchoTool()`

Returns a tool that echoes input text back.

```go
func ExampleEchoTool() agent.Tool
```

**Description:** Echoes the input text back to the caller
**Arguments:**
- `text` (string): The text to echo
**Returns:** ToolResult with the echoed text

**Usage Example:**
```go
echoTool := tools.ExampleEchoTool()
state := agent.NewAgentState(agent.AgentStateOptions{})
result, err := agent.ExecuteToolCall(ctx, &state, agent.ToolCallContent{
    ID:        "call-123",
    Name:      "echo",
    Arguments: map[string]any{"text": "Hello, World!"},
})
```

#### `ExampleMathTool()`

Returns a tool that performs simple math operations.

```go
func ExampleMathTool() agent.Tool
```

**Description:** Performs simple math operations: add, subtract, multiply, divide
**Arguments:**
- `operation` (string): The operation to perform
- `a` (float64): The first operand
- `b` (float64): The second operand
**Supported Operations:** add, subtract, multiply, divide
**Returns:** ToolResult with the result as a formatted string

**Usage Example:**
```go
mathTool := tools.ExampleMathTool()
result, err := agent.ExecuteToolCall(ctx, &state, agent.ToolCallContent{
    ID:        "call-456",
    Name:      "math",
    Arguments: map[string]any{"operation": "add", "a": 5, "b": 3},
})
```

#### `ExampleTimeTool()`

Returns a tool that returns the current time.

```go
func ExampleTimeTool() agent.Tool
```

**Description:** Returns the current time in RFC3339 format
**Arguments:** None
**Returns:** ToolResult with the current time
**Features:** Demonstrates context cancellation support

#### `ExampleDelayTool()`

Returns a tool that simulates long-running operations.

```go
func ExampleDelayTool() agent.Tool
```

**Description:** Waits for the specified duration in milliseconds
**Arguments:**
- `duration_ms` (int or float64): Milliseconds to wait (must be non-negative)
**Returns:** ToolResult with a message about the wait time
**Features:** Demonstrates proper context cancellation handling for long-running tools

### Tool Registry

#### `Registry`

`Registry` manages tool registration and lookup.

```go
type Registry struct {
    mu    sync.RWMutex
    tools map[string]agent.Tool
}
```

**Constructor and Methods:**
- `NewRegistry(tools ...agent.Tool) *Registry` - Creates a new tool registry, optionally initialized with tools
- `RegisterTool(r *Registry, name string, tool agent.Tool) bool` - Registers a tool in the registry. Returns true if successful, false if a tool with that name already exists
- `GetTool(r *Registry, name string) *agent.Tool` - Retrieves a tool by name from the registry. Returns nil if not found

#### `DefaultTools()`

Returns a slice of default example tools.

```go
func DefaultTools() []agent.Tool
```

**Returns:** A slice containing all example tools: echo, math, time, and delay

### Creating Custom Tools

To create a custom tool, implement the `agent.Tool` interface:

```go
type MyCustomTool struct {
    // Add custom fields if needed
}

func (t *MyCustomTool) Execute(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
    // Validate arguments
    if input, ok := args["input"].(string); !ok {
        return nil, fmt.Errorf("missing or invalid 'input' argument")
    }

    // Perform your tool's logic here
    result := fmt.Sprintf("Processed: %s", input)

    // Return the result
    return &agent.ToolResult{
        Content: []agent.ContentBlock{
            agent.TextContent{Text: result},
        },
        Details: map[string]any{
            "original_input": input,
            "timestamp": time.Now().Unix(),
        },
    }, nil
}

// Use the tool
tool := agent.Tool{
    Name:        "my_custom_tool",
    Description: "A custom tool that processes input",
    Execute:     (&MyCustomTool{}).Execute,
}
```

### Best Practices

1. **Validate Input Arguments:** Always validate input arguments and return clear error messages
2. **Handle Context Cancellation:** Check `ctx.Done()` in long-running operations
3. **Use Structured Results:** Return results as `ContentBlock` with additional details in `Details`
4. **Error Handling:** Use `ToolExecutionError` for tool-specific errors
5. **Tool Registration:** Use the `Registry` for managing multiple tools
6. **Event Handling:** Subscribe to events for real-time updates during execution

### Examples

#### Basic Agent Usage

```go
// Create an agent
agent := agent.NewAgent(agent.AgentOptions{
    SystemPrompt: "You are a helpful assistant",
    SessionID:    "session-123",
})

// Subscribe to events
subID, events := agent.Subscribe()
defer agent.Unsubscribe(subID)

// Process a message
err := agent.Prompt(context.Background(), "Hello, how are you?")
if err != nil {
    log.Fatal(err)
}

// Listen for events
for event := range events {
    switch e := event.(type) {
    case agent.AgentStartEvent:
        log.Printf("Agent started at %d", e.Timestamp)
    case agent.AgentEndEvent:
        log.Printf("Agent ended with %d messages", len(e.Messages))
    case agent.MessageEndEvent:
        log.Printf("Message received: %s", e.Message.Content[0])
    }
}
```

#### Tool Execution

```go
// Create a tool registry
registry := tools.NewRegistry(
    tools.ExampleEchoTool(),
    tools.ExampleMathTool(),
    tools.ExampleTimeTool(),
)

// Get a tool from registry
tool := tools.GetTool(registry, "echo")
if tool == nil {
    log.Fatal("Tool not found")
}

// Execute the tool
toolCall := agent.ToolCallContent{
    ID:        "call-123",
    Name:      "echo",
    Arguments: map[string]any{"text": "Hello from tool!"},
}

state := agent.NewAgentState(agent.AgentStateOptions{})
result, err := agent.ExecuteToolCall(context.Background(), &state, toolCall)
if err != nil {
    log.Printf("Tool execution failed: %v", err)
} else {
    fmt.Printf("Tool result: %s", result.Content[0].(agent.TextContent).Text)
}
```

#### Streaming Tool

```go
// Create a long-running tool
delayTool := tools.ExampleDelayTool()

// Use with context that can be cancelled
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

toolCall := agent.ToolCallContent{
    ID:        "call-delay",
    Name:      "delay",
    Arguments: map[string]any{"duration_ms": 10000}, // 10 seconds
}

// Context will cancel after 5 seconds
result, err := agent.ExecuteToolCall(ctx, &state, toolCall)
if err != nil {
    fmt.Printf("Tool cancelled: %v", err) // Will be context.DeadlineExceeded
} else {
    fmt.Printf("Tool completed: %s", result.Content[0].(agent.TextContent).Text)
}
```