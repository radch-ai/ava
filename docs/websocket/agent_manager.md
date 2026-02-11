# Agent Manager

The `AgentManager` manages per-connection agents with thread-safe access. Each WebSocket connection gets its own agent instance identified by session ID.

## Struct Definition

```go
type AgentManager struct {
    mu     sync.RWMutex
    agents map[string]*agent.Agent
    logger zerolog.Logger
}
```

## Constructor

```go
func NewAgentManager(logger zerolog.Logger) *AgentManager
```

Creates a new `AgentManager` with an empty agent map.

**Parameters:**
- `logger` - zerolog logger for structured logging

## Methods

### GetOrCreateAgent

```go
func (m *AgentManager) GetOrCreateAgent(sessionID string) *agent.Agent
```

Returns an existing agent for the session or creates a new one.

**Algorithm:**
1. Acquire read lock, check if agent exists
2. If found, return existing agent
3. Acquire write lock
4. Double-check that another goroutine didn't create it
5. Create new agent with session ID
6. Store in map and return

**Thread Safety:**
Uses double-checked locking pattern for efficiency - read lock for initial check, write lock only for creation.

**Agent Configuration:**
```go
newAgent := agent.NewAgent(agent.AgentOptions{
    SessionID: sessionID,
})
```

### RemoveAgent

```go
func (m *AgentManager) RemoveAgent(sessionID string)
```

Removes the agent for a given session and unsubscribes any event channels.

**Operations:**
1. Acquire write lock
2. Unsubscribe any event channels for this session
3. Delete agent from map

## Usage Pattern

Typical lifecycle in handler:
```go
// On connection
agent := h.agentManager.GetOrCreateAgent(sessionID)
subID, eventCh := agent.Subscribe()

// ... use agent ...

// On disconnect
agent.Unsubscribe(subID)
h.agentManager.RemoveAgent(sessionID)
```

## Thread Safety

All operations are thread-safe:
- `GetOrCreateAgent`: Uses read-write locks with double-checked locking
- `RemoveAgent`: Uses write lock exclusively

Multiple goroutines can safely access agents concurrently.

## Related

- [Handler](handler.md) - WebSocket handler that uses AgentManager
- [Protocol](protocol.md) - Event formats for agent communication
