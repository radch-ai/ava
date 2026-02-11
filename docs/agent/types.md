# Core Types

This document describes the core types used throughout the agent framework. These types are defined in `internal/agent/types.go`.

## Role

Role represents the sender of a message in the conversation.

```go
type Role string

const (
    RoleUser       Role = "user"
    RoleAssistant  Role = "assistant"
    RoleToolResult Role = "toolResult"
)
```

### Values

| Value | Description |
|-------|-------------|
| `RoleUser` | From the human user |
| `RoleAssistant` | From the AI agent |
| `RoleToolResult` | Result from tool execution |

## ContentBlock

ContentBlock is a sealed interface for different types of content in messages. The framework supports three content block types.

### TextContent

Plain text content in a message.

```go
type TextContent struct {
    Text string `json:"text"`
}
```

### ImageContent

Base64-encoded image data with MIME type.

```go
type ImageContent struct {
    Data     string `json:"data"`
    MimeType string `json:"mime_type"`
}
```

### ToolCallContent

Represents a tool invocation request from an assistant.

```go
type ToolCallContent struct {
    ID        string         `json:"id"`
    Name      string         `json:"name"`
    Arguments map[string]any `json:"arguments"`
}
```

| Field | Description |
|-------|-------------|
| `ID` | Unique identifier for this tool call |
| `Name` | Name of the tool to invoke |
| `Arguments` | Arguments to pass to the tool |

## AgentMessage

A message in the agent conversation with role, content blocks, and optional tool metadata.

```go
type AgentMessage struct {
    Role      Role           `json:"role"`
    Content   []ContentBlock `json:"content"`
    Timestamp int64          `json:"timestamp"`

    // ToolResult-specific fields
    ToolCallID string `json:"tool_call_id,omitempty"`
    ToolName   string `json:"tool_name,omitempty"`
    IsError    bool   `json:"is_error,omitempty"`
}
```

### Creating Messages

Helper functions are provided for creating different message types:

```go
// Create user message
userMsg := NewTextMessage(RoleUser, "Hello, how are you?")

// Create assistant message with tool call
toolCallMsg := NewToolCallMessage("call-123", "calculator", map[string]any{
    "expression": "2 + 2",
})

// Create tool result message
resultMsg := NewToolResultMessage("call-123", "calculator", "4", false)
```

## Model

Represents an LLM model configuration.

```go
type Model struct {
    ID       string
    API      string
    Provider string
    BaseURL  string
}
```

| Field | Description |
|-------|-------------|
| `ID` | Model identifier (e.g., "gpt-4", "claude-3-sonnet") |
| `API` | API endpoint path |
| `Provider` | Provider name ("openai", "anthropic", "google") |
| `BaseURL` | Custom base URL for API requests |

## ThinkingLevel

Represents the thinking/reasoning intensity level for models that support extended thinking.

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

### Methods

| Method | Description |
|---------|-------------|
| `IsOff() bool` | Returns true if thinking is disabled |
| `IsEnabled() bool` | Returns true if thinking is enabled (any non-off level) |

## Tool

Represents a callable tool function that can be invoked by the agent.

```go
type Tool struct {
    Name        string
    Description string
    Execute     func(ctx context.Context, args map[string]any) (*ToolResult, error)
}
```

| Field | Description |
|-------|-------------|
| `Name` | Unique identifier for the tool |
| `Description` | Human-readable description of what the tool does |
| `Execute` | Function that executes the tool with given arguments |

## ToolResult

Result of a tool execution with content blocks and optional details.

```go
type ToolResult struct {
    Content []ContentBlock
    Details any
}
```

| Field | Description |
|-------|-------------|
| `Content` | Result content as content blocks (typically TextContent) |
| `Details` | Optional structured data for programmatic access |

## AgentState

Tracks the current state of an agent session. See [agent.md](agent.md) for full details on state management.

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

### State Methods

| Method | Description |
|---------|-------------|
| `AddMessage(msg AgentMessage)` | Appends a message to conversation history |
| `SetStreaming(streaming bool, msg *AgentMessage)` | Updates streaming state |
| `AddPendingToolCall(id string)` | Marks a tool call as pending |
| `RemovePendingToolCall(id string)` | Removes tool call from pending set |
| `HasPendingToolCall(id string) bool` | Returns true if tool call is still pending |

### Creating State

```go
state := NewAgentState(AgentStateOptions{
    SystemPrompt:  "You are a helpful assistant",
    Model:         Model{ID: "gpt-4", Provider: "openai"},
    ThinkingLevel: ThinkingLevelMedium,
    Tools:         []Tool{ExampleEchoTool()},
    SessionID:     "session-123",
})
```

## See Also

- [agent.md](agent.md) - Agent struct and state management
- [events.md](events.md) - Event types for real-time monitoring
- [tool.go.md](tool.go.md) - Tool execution functions
