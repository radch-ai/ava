# Agent Core Documentation

The Agent Core is the runtime framework that handles AI agent state management, tool execution, and event streaming. This component has been ported from pi-agent-core (TypeScript) to Go, providing a robust foundation for building AI applications with tool calling capabilities.

## Overview

### What is the Agent Core?

The Agent Core is a Go implementation of the agent runtime framework that manages:
- **Tool execution**: Calling external functions and APIs on behalf of the AI
- **State management**: Maintaining conversation history and agent state
- **Event streaming**: Broadcasting real-time updates about agent operations

### What it does

The Agent Core handles:
- Message processing and conversation state
- Tool discovery and execution
- Event broadcasting for real-time updates
- Thread-safe state access
- Concurrent event subscriptions
- Context-aware prompt execution

### What it does NOT do

The Agent Core does NOT handle:
- LLM API calls (handled by a separate LLM proxy component)
- Authentication and authorization
- WebSocket communication (handled by the gateway server)
- Message routing or transport protocols
- Persistent storage
- Agent deployment or lifecycle management

## Architecture

### Component Diagram

```
┌─────────────────────────────────────────────────────────────┐
│                        Client Application                     │
│                                                             │
│  ┌─────────┐     ┌─────────────┐     ┌─────────────────┐  │
│  │  UI/Web │ <-> │   Gateway    │ <-> │   Agent Manager   │  │
│  │ Client │     │   Server     │     │     (optional)   │  │
│  └─────────┘     └─────────────┘     └─────────────────┘  │
│                                                             │
└─────────────────────────────────────────────────────────────┘
                            │
                            │ WebSocket
                            │
┌─────────────────────────────────────────────────────────────┐
│                    Agent Core Runtime                       │
│                                                             │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │                  Agent Instance                        │ │
│  │                                                         │ │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐ │ │
│  │  │ State   │  │ Events  │  │ Tools   │  │ Prompt  │ │ │
│  │  │ Manager │  │ Broker   │  │ Registry│  │ Handler │ │ │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘ │ │
│  │                                                         │ │
│  │  ┌─────────┐  ┌─────────┐                              │ │
│  │  │Message  │  │Tool     │                              │ │
│  │  │History  │  │Executor │                              │ │
│  │  └─────────┘  └─────────┘                              │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                             │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │                   Event Types                          │ │
│  │                                                         │ │
│  │  AgentStart  AgentEnd   TurnStart  TurnEnd             │ │
│  │  MessageStart MessageUpdate MessageEnd                  │ │
│  │  ToolExecutionStart ToolExecutionUpdate ToolExecutionEnd │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                            │
                            │ HTTP/gRPC
                            │
┌─────────────────────────────────────────────────────────────┐
│                    LLM Proxy (External)                    │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │             OpenAI, Anthropic, etc.                    │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### Key Types and Relationships

```go
// Agent is the main controller
type Agent struct {
    mu           sync.RWMutex           // Thread-safe state access
    state        AgentState             // Agent state and conversation history
    id           string                 // Unique agent identifier
    subscribers  map[string]chan AgentEvent // Event subscribers
    ctx          context.Context        // Context for cancellation
    cancel       context.CancelFunc     // Cancel function
}

// AgentState holds the current state
type AgentState struct {
    SystemPrompt     string        // System instruction
    Model            Model         // LLM model configuration
    ThinkingLevel    ThinkingLevel // Reasoning intensity
    Tools            []Tool        // Available tools
    Messages         []AgentMessage // Conversation history
    IsStreaming      bool          // Streaming state
    StreamMessage    *AgentMessage // Current stream message
    PendingToolCalls map[string]bool // Tool calls in progress
    Error            string        // Last error state
    SessionID        string        // Session identifier
}

// Tool represents an executable function
type Tool struct {
    Name        string                                 // Tool name
    Description string                                 // Tool description
    Execute     func(ctx context.Context, args map[string]any) (*ToolResult, error)
}

// AgentMessage represents a conversation message
type AgentMessage struct {
    Role      Role           `json:"role"`      // Message sender role
    Content   []ContentBlock `json:"content"`   // Message content blocks
    Timestamp int64          `json:"timestamp"` // Unix timestamp

    // Tool result fields
    ToolCallID string `json:"tool_call_id,omitempty"`
    ToolName   string `json:"tool_name,omitempty"`
    IsError    bool   `json:"is_error,omitempty"`
}
```

### Event Flow

1. **Client sends prompt** → Agent processes user message
2. **AgentStartEvent** → Broadcasts agent start
3. **TurnStartEvent** → New interaction cycle begins
4. **MessageStartEvent** → Message generation starts
5. **LLM API call** → External proxy call
6. **Tool calls detected** → ToolExecutionStartEvent emitted
7. **Tools execute** → ToolExecutionUpdate/End events
8. **Message completion** → MessageEndEvent
9. **Turn completion** → TurnEndEvent
10. **Agent completion** → AgentEndEvent

## Message Types

### Role Enum

```go
type Role string

const (
    RoleUser       Role = "user"       // From the human user
    RoleAssistant  Role = "assistant"  // From the AI agent
    RoleToolResult Role = "toolResult" // From tool execution
)
```

### ContentBlock Interface

The ContentBlock interface supports multiple content types:

```go
// TextContent represents plain text
type TextContent struct {
    Text string `json:"text"`
}

// ImageContent represents base64-encoded image data
type ImageContent struct {
    Data     string `json:"data"`
    MimeType string `json:"mime_type"`
}

// ToolCallContent represents a tool invocation request
type ToolCallContent struct {
    ID        string         `json:"id"`
    Name      string         `json:"name"`
    Arguments map[string]any `json:"arguments"`
}
```

### AgentMessage Structure

```go
type AgentMessage struct {
    Role      Role           `json:"role"`
    Content   []ContentBlock `json:"content"`
    Timestamp int64          `json:"timestamp"`

    // Tool result metadata
    ToolCallID string `json:"tool_call_id,omitempty"`
    ToolName   string `json:"tool_name,omitempty"`
    IsError    bool   `json:"is_error,omitempty"`
}
```

## Agent Lifecycle

### Creating an Agent

```go
// Create a new agent with options
agent := NewAgent(AgentOptions{
    SystemPrompt: "You are a helpful assistant",
    SessionID:    "session-123",
})

// Or with default values
agent := NewAgent(AgentOptions{
    ID:           "agent-1",          // Optional (auto-generated if empty)
    SystemPrompt: "You are a helpful assistant",
    SessionID:    "session-123",
})
```

### Subscribing to Events

```go
// Subscribe to agent events
subID, events := agent.Subscribe()
defer agent.Unsubscribe(subID)

// Process events
for event := range events {
    switch e := event.(type) {
    case AgentStartEvent:
        fmt.Printf("Agent started at: %d\n", e.Timestamp)
    case AgentEndEvent:
        fmt.Printf("Agent completed with %d messages\n", len(e.Messages))
    case ToolExecutionEndEvent:
        fmt.Printf("Tool %s completed: %v\n", e.ToolName, e.Result)
    }
}
```

### Sending Prompts

```go
// Send a user message to the agent
ctx := context.Background()
err := agent.Prompt(ctx, "Hello, how are you?")
if err != nil {
    log.Printf("Error prompting agent: %v", err)
}
```

### Tool Execution Flow

```go
// 1. Agent receives tool calls from LLM
// 2. ToolExecutionStartEvent emitted
// 3. Tool.execute() called with context and arguments
// 4. ToolExecutionUpdateEvent for partial results (if any)
// 5. ToolExecutionEndEvent emitted with final result
// 6. Result added to conversation history
```

## Event Reference

### 1. AgentStartEvent

Emitted when the agent starts processing a prompt.

```go
type AgentStartEvent struct {
    Timestamp int64 `json:"timestamp"` // Unix timestamp when agent started
}
```

**When emitted**: Immediately when `Prompt()` is called.

### 2. AgentEndEvent

Emitted when the agent finishes processing a prompt.

```go
type AgentEndEvent struct {
    Timestamp int64          `json:"timestamp"` // Completion timestamp
    Messages  []AgentMessage `json:"messages"`  // Complete conversation history
}
```

**When emitted**: After all tool execution is complete.

### 3. TurnStartEvent

Emitted when a new interaction cycle (turn) begins.

```go
type TurnStartEvent struct {
    Timestamp int64 `json:"timestamp"` // Turn start timestamp
}
```

**When emitted**: At the start of each tool calling cycle.

### 4. TurnEndEvent

Emitted when a turn completes.

```go
type TurnEndEvent struct {
    Timestamp  int64          `json:"timestamp"`    // Turn end timestamp
    Message    AgentMessage   `json:"message"`     // Final assistant message
    ToolResults []AgentMessage `json:"tool_results"` // Tool execution results
}
```

**When emitted**: After tool execution and message completion.

### 5. MessageStartEvent

Emitted when message generation starts.

```go
type MessageStartEvent struct {
    Timestamp int64          `json:"timestamp"` // Message start timestamp
    Message   AgentMessage   `json:"message"`   // Message being generated
}
```

**When emitted**: When the LLM begins generating a response.

### 6. MessageUpdateEvent

Emitted when streaming content is added to a message.

```go
type MessageUpdateEvent struct {
    Timestamp int64 `json:"timestamp"` // Update timestamp
    Message   AgentMessage `json:"message"` // Current message state
    Delta     string `json:"delta"`     // New content delta
}
```

**When emitted**: During streaming responses with new content.

### 7. MessageEndEvent

Emitted when a message is complete.

```go
type MessageEndEvent struct {
    Timestamp int64          `json:"timestamp"` // Completion timestamp
    Message   AgentMessage   `json:"message"`   // Complete message
}
```

**When emitted**: After message generation is complete.

### 8. ToolExecutionStartEvent

Emitted when tool execution begins.

```go
type ToolExecutionStartEvent struct {
    Timestamp  int64               `json:"timestamp"`  // Start timestamp
    ToolCallID string              `json:"tool_call_id"` // Tool call identifier
    ToolName   string              `json:"tool_name"`   // Tool name
    Args       map[string]any      `json:"args"`        // Tool arguments
}
```

**When emitted**: When a tool is about to be executed.

### 9. ToolExecutionUpdateEvent

Emitted when a tool produces partial results during execution.

```go
type ToolExecutionUpdateEvent struct {
    Timestamp     int64 `json:"timestamp"`     // Update timestamp
    ToolCallID    string `json:"tool_call_id"` // Tool call identifier
    ToolName      string `json:"tool_name"`   // Tool name
    PartialResult any    `json:"partial_result"` // Partial result data
}
```

**When emitted**: During long-running tool operations with progress updates.

### 10. ToolExecutionEndEvent

Emitted when tool execution completes.

```go
type ToolExecutionEndEvent struct {
    Timestamp  int64          `json:"timestamp"`  // Completion timestamp
    ToolCallID string         `json:"tool_call_id"` // Tool call identifier
    ToolName   string         `json:"tool_name"`   // Tool name
    Result     any            `json:"result"`     // Final result
    IsError    bool           `json:"is_error"`   // Whether execution failed
}
```

**When emitted**: After tool execution completes successfully or with error.

## Tool Interface

### Creating Tools

Tools implement the `Tool` interface:

```go
type Tool struct {
    Name        string                                 // Tool name (required)
    Description string                                 // Tool description (required)
    Execute     func(ctx context.Context, args map[string]any) (*ToolResult, error) // Tool implementation
}

type ToolResult struct {
    Content []ContentBlock `json:"content"` // Result content
    Details any            `json:"details"` // Additional details (optional)
}
```

### Example Tool Implementation

```go
// Create a calculator tool
calculatorTool := Tool{
    Name:        "calculator",
    Description: "Performs basic arithmetic operations",
    Execute: func(ctx context.Context, args map[string]any) (*ToolResult, error) {
        // Parse arguments
        a, ok := args["a"].(float64)
        if !ok {
            return nil, fmt.Errorf("missing or invalid 'a' argument")
        }

        b, ok := args["b"].(float64)
        if !ok {
            return nil, fmt.Errorf("missing or invalid 'b' argument")
        }

        operation, ok := args["operation"].(string)
        if !ok {
            return nil, fmt.Errorf("missing 'operation' argument")
        }

        // Perform calculation
        var result float64
        switch operation {
        case "add":
            result = a + b
        case "subtract":
            result = a - b
        case "multiply":
            result = a * b
        case "divide":
            if b == 0 {
                return nil, fmt.Errorf("division by zero")
            }
            result = a / b
        default:
            return nil, fmt.Errorf("unknown operation: %s", operation)
        }

        // Return result
        return &ToolResult{
            Content: []ContentBlock{TextContent{Text: fmt.Sprintf("%.2f", result)}},
            Details: result,
        }, nil
    },
}
```

### Tool Execution Model

1. **Discovery**: Agent finds tool by name in available tools
2. **Validation**: Checks for missing/invalid arguments
3. **Execution**: Calls tool's `Execute` function with context
4. **Result**: Tool returns `ToolResult` or error
5. **Conversion**: Result converted to `AgentMessage` with `RoleToolResult`

### Error Handling

```go
// Tools should return errors for invalid input
if value < 0 {
    return nil, fmt.Errorf("value must be non-negative, got %v", value)
}

// Tool execution errors are wrapped in ToolExecutionError
type ToolExecutionError struct {
    ToolName string
    Message  string
}

// Error includes tool name for better debugging
func (e *ToolExecutionError) Error() string {
    return fmt.Sprintf("tool %s: %s", e.ToolName, e.Message)
}
```

### Context Support

```go
// Tools should respect context cancellation
func (t *Tool) Execute(ctx context.Context, args map[string]any) (*ToolResult, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    // Continue with execution...
}
```

## Usage Examples

### Creating an Agent with Tools

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/nevaria-ai/gateway/internal/agent"
    "github.com/nevaria-ai/gateway/internal/agent/tools"
)

func main() {
    // Create a new agent
    agent := agent.NewAgent(agent.AgentOptions{
        SystemPrompt: "You are a helpful assistant with access to tools.",
        SessionID:    "session-" + time.Now().Format("20060102150405"),
    })

    // Add tools to the agent state
    state := agent.State()
    state.Tools = []agent.Tool{
        tools.ExampleEchoTool(),
        tools.ExampleMathTool(),
        tools.ExampleTimeTool(),
    }

    // Update agent state (in production, use proper locking)
    // Note: In a real implementation, you'd need to lock the agent state
    // before modifying it to maintain thread safety.
}
```

### Handling Events

```go
package main

import (
    "fmt"
    "log"
    "time"

    "github.com/nevaria-ai/gateway/internal/agent"
)

func main() {
    // Create agent
    agent := agent.NewAgent(agent.AgentOptions{
        SystemPrompt: "You are a helpful assistant",
        SessionID:    "demo-session",
    })

    // Subscribe to events
    subID, events := agent.Subscribe()
    defer agent.Unsubscribe(subID)

    // Process events in a separate goroutine
    go func() {
        for event := range events {
            switch e := event.(type) {
            case agent.AgentStartEvent:
                log.Printf("Agent started at: %d", e.Timestamp)

            case agent.MessageStartEvent:
                log.Printf("Message started: %s", e.Message.Role)

            case agent.MessageUpdateEvent:
                log.Printf("Message update: %s", e.Delta)

            case agent.ToolExecutionStartEvent:
                log.Printf("Tool execution started: %s(%v)", e.ToolName, e.Args)

            case agent.ToolExecutionEndEvent:
                if e.IsError {
                    log.Printf("Tool failed: %s - %v", e.ToolName, e.Result)
                } else {
                    log.Printf("Tool completed: %s - %v", e.ToolName, e.Result)
                }

            case agent.AgentEndEvent:
                log.Printf("Agent completed with %d messages", len(e.Messages))
            }
        }
    }()

    // Send a prompt
    ctx := context.Background()
    err := agent.Prompt(ctx, "What is the current time?")
    if err != nil {
        log.Printf("Error: %v", err)
    }

    // Wait for completion
    time.Sleep(2 * time.Second)
}
```

### Multi-turn Conversations

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/nevaria-ai/gateway/internal/agent"
    "github.com/nevaria-ai/gateway/internal/agent/tools"
)

func main() {
    // Create agent with math tools
    agent := agent.NewAgent(agent.AgentOptions{
        SystemPrompt: "You are a math assistant. Use the calculator tool for math problems.",
        SessionID:    "math-session",
    })

    // Add math tool
    state := agent.State()
    state.Tools = []agent.Tool{tools.ExampleMathTool()}

    // Multi-turn conversation
    prompts := []string{
        "What is 15 + 27?",
        "Multiply that result by 3",
        "Divide the answer by 2",
    }

    for _, prompt := range prompts {
        log.Printf("User: %s", prompt)

        err := agent.Prompt(context.Background(), prompt)
        if err != nil {
            log.Printf("Error: %v", err)
            continue
        }

        // Get agent response
        state := agent.State()
        if len(state.Messages) > 0 {
            lastMsg := state.Messages[len(state.Messages)-1]
            if lastMsg.Role == agent.RoleAssistant {
                for _, content := range lastMsg.Content {
                    if text, ok := content.(agent.TextContent); ok {
                        log.Printf("Assistant: %s", text.Text)
                        break
                    }
                }
            }
        }

        time.Sleep(1 * time.Second)
    }
}
```

### Tool Registry Pattern

```go
package main

import (
    "github.com/nevaria-ai/gateway/internal/agent"
    "github.com/nevaria-ai/gateway/internal/agent/tools"
)

// ToolRegistry manages tool collection
type ToolRegistry struct {
    tools map[string]agent.Tool
}

func NewToolRegistry() *ToolRegistry {
    return &ToolRegistry{
        tools: make(map[string]agent.Tool),
    }
}

func (r *ToolRegistry) Register(tool agent.Tool) {
    r.tools[tool.Name] = tool
}

func (r *ToolRegistry) Get(name string) *agent.Tool {
    if tool, exists := r.tools[name]; exists {
        return &tool
    }
    return nil
}

func (r *ToolRegistry) All() []agent.Tool {
    all := make([]agent.Tool, 0, len(r.tools))
    for _, tool := range r.tools {
        all = append(all, tool)
    }
    return all
}

func main() {
    // Create registry
    registry := NewToolRegistry()

    // Register built-in tools
    for _, tool := range tools.DefaultTools() {
        registry.Register(tool)
    }

    // Register custom tool
    registry.Register(agent.Tool{
        Name:        "weather",
        Description: "Get weather information for a city",
        Execute: func(ctx context.Context, args map[string]any) (*agent.ToolResult, error) {
            city := args["city"].(string)
            // Implement weather API call
            return &agent.ToolResult{
                Content: []agent.ContentBlock{agent.TextContent{Text: "Weather in " + city + ": Sunny, 72°F"}},
            }, nil
        },
    })

    // Create agent with registry tools
    agent := agent.NewAgent(agent.AgentOptions{
        SystemPrompt: "You have access to various tools.",
    })

    state := agent.State()
    state.Tools = registry.All()
    // Update agent state with proper locking in production
}
```

## Best Practices

1. **Thread Safety**: Always use proper locking when accessing agent state
2. **Context Cancellation**: Respect context deadlines in tool implementations
3. **Error Handling**: Provide clear error messages in tool implementations
4. **Event Subscription**: Always unsubscribe to prevent memory leaks
5. **Tool Validation**: Validate tool arguments before execution
6. **Streaming**: Use streaming events for real-time UI updates
7. **Resource Management**: Clean up resources in tool implementations
8. **Testing**: Test tools with various input combinations

## Integration Notes

- The Agent Core is designed to work with external LLM proxies
- WebSocket handlers integrate with the Agent for real-time communication
- Tool execution is fully asynchronous with event-based notifications
- State management supports both in-memory and future persistence layers
- The architecture supports scaling with multiple agent instances