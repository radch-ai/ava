# Agent

The Agent type manages AI agent state with thread-safe access and event broadcasting. This document covers the Agent struct, its methods, and subscription management.

## Agent Struct

```go
type Agent struct {
    mu            sync.RWMutex
    state         AgentState
    id            string
    subscribers   map[string]chan AgentEvent
    subscribersMu sync.Mutex
    ctx           context.Context
    cancel        context.CancelFunc
}
```

### Fields

| Field | Description |
|-------|-------------|
| `mu` | Read-write mutex for thread-safe state access |
| `state` | Current agent state (messages, tools, configuration) |
| `id` | Unique identifier for this agent instance |
| `subscribers` | Map of subscriber ID to event channels |
| `subscribersMu` | Mutex for protecting subscribers map |
| `ctx` | Context for agent lifecycle management |
| `cancel` | Cancel function for agent context |

## Creating an Agent

### NewAgent

Creates a new Agent with the given options. If no ID is provided, a UUID will be generated.

```go
type AgentOptions struct {
    ID           string
    SystemPrompt string
    SessionID    string
}

func NewAgent(opts AgentOptions) *Agent
```

#### Example

```go
agent := NewAgent(AgentOptions{
    SystemPrompt: "You are a helpful assistant",
    SessionID:    "session-123",
})
```

### NewAgentState

Creates a new AgentState with the given options.

```go
type AgentStateOptions struct {
    SystemPrompt  string
    Model         Model
    ThinkingLevel ThinkingLevel
    Tools         []Tool
    SessionID     string
}

func NewAgentState(opts AgentStateOptions) AgentState
```

## Event Subscription

### Subscribe

Registers a new event channel and returns its subscription ID.

```go
func (a *Agent) Subscribe() (string, chan AgentEvent)
```

Returns a tuple of:
- `subID`: Unique subscription identifier
- `ch`: Buffered channel (buffer size: 100) for receiving events

#### Example

```go
subID, events := agent.Subscribe()
defer agent.Unsubscribe(subID)

for event := range events {
    switch e := event.(type) {
    case AgentStartEvent:
        log.Printf("Agent started at %d", e.Timestamp)
    case MessageEndEvent:
        log.Printf("Message completed: %v", e.Message)
    }
}
```

### Unsubscribe

Removes the event channel for a given subscription ID. The channel is closed before removal.

```go
func (a *Agent) Unsubscribe(id string)
```

If the subscription ID is not found, this is a no-op.

## State Access

### State

Returns a deep copy of the current agent state. This prevents external mutation of internal state.

```go
func (a *Agent) State() AgentState
```

The returned state includes:
- Deep-copied message history
- Deep-copied pending tool calls map
- Deep-copied tools slice

#### Example

```go
state := agent.State()
fmt.Printf("Messages: %d\n", len(state.Messages))
fmt.Printf("Tools: %d\n", len(state.Tools))
```

## Message Processing

### Prompt

Sends a user message to the agent and runs the tool calling loop.

```go
func (a *Agent) Prompt(ctx context.Context, message string) error
```

#### Execution Flow

1. Validate context (return error if cancelled)
2. Lock and add user message to `state.Messages`
3. Emit `AgentStartEvent`
4. Set streaming state to `true`
5. Run tool calling loop
6. Emit `AgentEndEvent` with final messages
7. Clear streaming state

#### Example

```go
ctx := context.Background()
err := agent.Prompt(ctx, "What's the weather in NYC?")
if err != nil {
    log.Fatal(err)
}
```

### runToolCallingLoop

Internal method that processes the LLM response and handles tool calls. In the current implementation, this is a placeholder that simulates a response.

The full implementation will:
- Call the LLM with current messages and tools
- Handle tool calls if any are returned
- Emit appropriate events (TurnStart, TurnEnd, MessageStart, etc.)
- Continue until LLM responds without tool calls or context is cancelled

## Agent State

The AgentState struct tracks all mutable state for an agent session.

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

### State Fields

| Field | Description |
|-------|-------------|
| `SystemPrompt` | System prompt sent to LLM |
| `Model` | LLM model configuration |
| `ThinkingLevel` | Thinking intensity level |
| `Tools` | Available tools for the agent |
| `Messages` | Conversation history |
| `IsStreaming` | Whether agent is currently streaming |
| `StreamMessage` | Current message being streamed |
| `PendingToolCalls` | Set of tool call IDs awaiting results |
| `Error` | Last error message |
| `SessionID` | Session identifier |

### State Methods

| Method | Description |
|---------|-------------|
| `AddMessage(msg AgentMessage)` | Appends a message to conversation history |
| `SetStreaming(streaming bool, msg *AgentMessage)` | Updates streaming state |
| `AddPendingToolCall(id string)` | Marks a tool call as pending |
| `RemovePendingToolCall(id string)` | Removes tool call from pending set |
| `HasPendingToolCall(id string) bool` | Returns true if tool call is still pending |

## Configuration

### AgentOptions

Configuration options for creating a new agent.

```go
type AgentOptions struct {
    ID           string
    SystemPrompt string
    SessionID    string
}
```

| Field | Description | Default |
|-------|-------------|---------|
| `ID` | Agent identifier | Auto-generated UUID |
| `SystemPrompt` | System prompt for LLM | Empty |
| `SessionID` | Session identifier | Empty |

### AgentStateOptions

Configuration options for creating agent state.

```go
type AgentStateOptions struct {
    SystemPrompt  string
    Model         Model
    ThinkingLevel ThinkingLevel
    Tools         []Tool
    SessionID     string
}
```

## See Also

- [types.md](types.md) - Core type definitions
- [events.md](events.md) - Event types for subscription
- [loop.md](loop.md) - Tool calling loop implementation
