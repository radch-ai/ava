# Agent Manager

The \`AgentManager\` manages per-connection agents with thread-safe access. Each WebSocket connection gets its own agent instance identified by session ID.

**Package:** \`internal/websocket\`

## Struct Definition

\`\`\`go
type AgentManager struct {
    mu           sync.RWMutex
    agents       map[string]*agent.Agent
    logger       zerolog.Logger
    proxyHandler *proxy.Handler
}

type AgentManagerOptions struct {
    ProxyHandler *proxy.Handler
}
\`\`\`

## Constructor

\`\`\`go
func NewAgentManager(logger zerolog.Logger, opts *AgentManagerOptions) *AgentManager
\`\`\`

Creates a new \`AgentManager\` with an empty agent map.

**Parameters:**
- \`logger\` - zerolog logger for structured logging
- \`opts\` - Optional configuration including proxy handler

## Methods

### GetOrCreateAgent

\`\`\`go
func (m *AgentManager) GetOrCreateAgent(sessionID string, userID string) *agent.Agent
\`\`\`

Returns an existing agent for the session or creates a new one.

**Parameters:**
- \`sessionID\` - Unique session identifier
- \`userID\` - User identifier for quota tracking

**Algorithm:**
1. Acquire read lock, check if agent exists
2. If found, return existing agent
3. Acquire write lock
4. Double-check that another goroutine didn't create it
5. Create new agent with appropriate LLM client
6. Store in map and return

**Thread Safety:**
Uses double-checked locking pattern for efficiency - read lock for initial check, write lock only for creation.

**Agent Configuration:**
When \`ProxyHandler\` is configured, agents use \`proxy.Client\` for LLM calls. Otherwise uses \`MockLLMClient\` for testing.

\`\`\`go
// With proxy
proxyClient := proxy.NewClient(m.proxyHandler, userID)
opts := agent.AgentOptions{
    SessionID: sessionID,
    LLMClient: proxyClient,
}

// Without proxy (backward compatibility)
opts := agent.AgentOptions{
    SessionID: sessionID,
    LLMClient: &agent.MockLLMClient{},
}
\`\`\`

### RemoveAgent

\`\`\`go
func (m *AgentManager) RemoveAgent(sessionID string)
\`\`\`

Removes the agent for a given session and unsubscribes any event channels.

**Operations:**
1. Acquire write lock
2. Unsubscribe any event channels for this session
3. Delete agent from map

## Usage Pattern

Typical lifecycle in handler:
\`\`\`go
import "github.com/nevaria-ai/gateway/internal/websocket"

// Create manager with proxy support
mgr := websocket.NewAgentManager(logger, &websocket.AgentManagerOptions{
    ProxyHandler: proxyHandler,
})

// On connection
agentInstance := mgr.GetOrCreateAgent(sessionID, userID)
subID, eventCh := agentInstance.Subscribe()

// ... use agent ...

// On disconnect
agentInstance.Unsubscribe(subID)
mgr.RemoveAgent(sessionID)
\`\`\`

## Thread Safety

All operations are thread-safe:
- \`GetOrCreateAgent\`: Uses read-write locks with double-checked locking
- \`RemoveAgent\`: Uses write lock exclusively

Multiple goroutines can safely access agents concurrently.

## Related

- [Handler](handler.md) - WebSocket handler that uses AgentManager
- [Protocol](protocol.md) - Event formats for agent communication
- [Agent Package](../agent/) - Core agent runtime
