# Agent Package

Core agent runtime framework with state management, event streaming, and tool execution loop.

## Overview

```mermaid
flowchart TB
    Prompt[Agent.Prompt] --> AddMessage[Add User Message]
    AddMessage --> EmitStart[Emit AgentStartEvent]
    EmitStart --> RunLoop[Run Tool Loop]
    RunLoop --> Stream[Stream LLM Response]
    Stream --> EmitMessage[Emit Message Events]
    EmitMessage --> ToolCalls{Tool Calls?}
    ToolCalls -->|Yes| ExecuteTools[Execute Tools]
    ExecuteTools --> AddResults[Add Tool Results]
    AddResults --> RunLoop
    ToolCalls -->|No| UpdateState[Update State]
    UpdateState --> EmitEnd[Emit AgentEndEvent]
```

## Purpose

The agent package provides:

- Agent state management with thread-safe access
- Event subscription system (10 event types)
- Tool calling loop with parallel execution
- Message history tracking
- LLM client integration

## Types

### Agent

```go
type Agent struct {
    mu         sync.RWMutex
    state      AgentState
    id         string
    subscribers map[string]chan AgentEvent
    subscribersMu sync.Mutex
    ctx        context.Context
    cancel     context.CancelFunc
    llmClient  LLMClient
}
```

| Field | Type | Description |
|-------|------|-------------|
| `mu` | sync.RWMutex | State mutex |
| `state` | AgentState | Current agent state |
| `id` | string | Unique agent identifier |
| `subscribers` | map[string]chan AgentEvent | Event subscribers |
| `ctx` | context.Context | Agent context |
| `cancel` | context.CancelFunc | Context cancel function |
| `llmClient` | LLMClient | Optional LLM client |

### AgentState

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

| Field | Type | Description |
|-------|------|-------------|
| `SystemPrompt` | string | System prompt for LLM |
| `Model` | Model | Model configuration |
| `ThinkingLevel` | ThinkingLevel | Thinking intensity |
| `Tools` | []Tool | Available tools |
| `Messages` | []AgentMessage | Conversation history |
| `IsStreaming` | bool | Currently streaming |
| `StreamMessage` | *AgentMessage | Current streaming message |
| `PendingToolCalls` | map[string]bool | Pending tool call IDs |
| `Error` | string | Error state |
| `SessionID` | string | Session identifier |

### AgentMessage

```go
type AgentMessage struct {
    Role       Role
    Content    []ContentBlock
    Timestamp  int64
    ToolCallID string
    ToolName   string
    IsError    bool
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Role` | Role | Message role (user/assistant/toolResult) |
| `Content` | []ContentBlock | Message content |
| `Timestamp` | int64 | Unix timestamp |
| `ToolCallID` | string | Tool call ID (for tool results) |
| `ToolName` | string | Tool name (for tool results) |
| `IsError` | bool | Error flag (for tool results) |

### Tool

```go
type Tool struct {
    Name        string
    Description string
    Execute     func(ctx context.Context, args map[string]any) (string, error)
}
```

| Field | Type | Description |
|-------|------|-------------|
| `Name` | string | Tool identifier |
| `Description` | string | Tool description for LLM |
| `Execute` | func | Tool execution function |

## Roles

| Role | Description |
|------|-------------|
| `RoleUser` | From user |
| `RoleAssistant` | From AI assistant |
| `RoleToolResult` | From tool execution |

## Thinking Levels

| Level | Description |
|-------|-------------|
| `ThinkingLevelOff` | Disabled |
| `ThinkingLevelMinimal` | Minimal |
| `ThinkingLevelLow` | Low |
| `ThinkingLevelMedium` | Medium |
| `ThinkingLevelHigh` | High |

## Functions

| Function | Description |
|----------|-------------|
| `NewAgent(opts AgentOptions) *Agent` | Creates new agent |
| `Subscribe() (string, chan AgentEvent)` | Subscribe to events |
| `Unsubscribe(id string)` | Unsubscribe from events |
| `State() AgentState` | Gets current state (copy) |
| `Prompt(ctx context.Context, message string) error` | Sends user message |

### State Management

| Function | Description |
|----------|-------------|
| `NewAgentState(opts AgentStateOptions) AgentState` | Creates state |
| `AddMessage(msg AgentMessage)` | Adds message to history |
| `SetStreaming(streaming bool, msg *AgentMessage)` | Sets streaming state |
| `AddPendingToolCall(id string)` | Marks tool call as pending |
| `RemovePendingToolCall(id string)` | Removes pending tool call |

### Tools

| Function | Description |
|----------|-------------|
| `ExecuteToolCall(ctx context.Context, state *AgentState, toolCall ToolCallContent) (AgentMessage, error)` | Executes tool |
| `FindToolByName(state *AgentState, name string) *Tool` | Finds tool by name |

### Messages

| Function | Description |
|----------|-------------|
| `NewTextMessage(role Role, text string) AgentMessage` | Creates text message |
| `NewToolCallMessage(id, name string, args map[string]any) AgentMessage` | Creates tool call message |
| `NewToolResultMessage(toolCallID, toolName, result string, isError bool) AgentMessage` | Creates tool result message |

## Event Types

| Event | Description |
|-------|-------------|
| `AgentStartEvent` | Agent begins processing |
| `AgentEndEvent` | Agent finishes processing |
| `TurnStartEvent` | New interaction cycle begins |
| `TurnEndEvent` | Interaction cycle completes |
| `MessageStartEvent` | Message generation starts |
| `MessageUpdateEvent` | Streaming content delta |
| `MessageEndEvent` | Message is complete |
| `ToolExecutionStartEvent` | Tool execution begins |
| `ToolExecutionUpdateEvent` | Partial tool result |
| `ToolExecutionEndEvent` | Tool execution completes |

## Flow

### Prompt Execution

```mermaid
sequenceDiagram
    participant C as Caller
    participant A as Agent
    participant L as Loop
    participant LLM as LLM Client

    C->>A: Prompt(message)
    A->>A: Add user message to state
    A->>A: Emit AgentStartEvent
    A->>L: NewLoop(llmClient, state)
    L->>LLM: Stream completion
    loop LLM Streaming
        LLM-->>L: Content delta
        L->>A: Emit MessageUpdateEvent
    end
    L->>L: Extract tool calls
    loop Has Tool Calls
        L->>L: Execute tools
        L->>L: Add tool results
        L->>LLM: Stream completion
    end
    L-->>A: Done
    A->>A: Emit AgentEndEvent
```

## Sub-packages

| Package | Description |
|---------|-------------|
| [tools.md](tools.md) | Tool interface and execution framework |
| [types.md](types.md) | Agent types and event definitions |

## File Locations

- [`internal/agent/agent.go`](../internal/agent/agent.go) - Main agent implementation
- [`internal/agent/loop.go`](../internal/agent/loop.go) - Tool calling loop
- [`internal/agent/tool.go`](../internal/agent/tool.go) - Tool execution
- [`internal/agent/types.go`](../internal/agent/types.go) - Core types
- [`internal/agent/events.go`](../internal/agent/events.go) - Event definitions
- [`tests/unit/agent/`](../tests/unit/agent/) - Tests
