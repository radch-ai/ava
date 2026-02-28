// Package agent provides the core agent runtime with event subscription.
//
// The agent runtime manages AI agent state, tool execution, and event broadcasting.
// It supports thread-safe operations, concurrent event subscriptions, and context-aware
// prompt execution with tool calling.
//
// # Creating an Agent
//
// Create a new agent with NewAgent:
//
//	agent := NewAgent(AgentOptions{
//	    SystemPrompt: "You are a helpful assistant",
//	    SessionID:    "session-123",
//	})
//
// # Adding Tools
//
// Tools can be added to the agent's state:
//
//	state := agent.State()
//	state.Tools = append(state.Tools, ExampleTool())
//	// Update agent state (requires locking in real implementation)
//
// See the tools subpackage for example tools and a tool registry.
//
// # Subscribing to Events
//
// Subscribe to agent events to receive real-time updates:
//
//	subID, events := agent.Subscribe()
//	defer agent.Unsubscribe(subID)
//
//	for event := range events {
//	    switch e := event.(type) {
//	    case AgentStartEvent:
//	        // Handle agent start
//	    case AgentEndEvent:
//	        // Handle agent end
//	    case MessageStartEvent:
//	        // Handle message generation start
//	    // ... other event types
//	    }
//	}
package agent

import (
	"sync"

	"github.com/google/uuid"
)

const defaultEventChannelBuffer = 100

// AgentOptions configures a new Agent.
type AgentOptions struct {
	ID           string
	SystemPrompt string
	SessionID    string
	UserID       string
	AgentID      string
}

// Agent manages agent state with thread-safe access and event broadcasting.
type Agent struct {
	mu            sync.RWMutex
	state         AgentState
	id            string
	userID        string
	agentID       string
	subscribers   map[string]chan AgentEvent
	subscribersMu sync.Mutex
	lane          *Lane
}

// NewAgent creates a new Agent with the given options.
// If no ID is provided, a UUID will be generated.
// If no UserID is provided, it defaults to "unknown".
// If no AgentID is provided, it defaults to the agent's ID.
func NewAgent(opts AgentOptions) *Agent {
	id := opts.ID
	if id == "" {
		id = uuid.New().String()
	}

	userID := opts.UserID
	if userID == "" {
		userID = "unknown"
	}

	agentID := opts.AgentID
	if agentID == "" {
		agentID = id
	}

	state := NewAgentState(AgentStateOptions{
		SystemPrompt: opts.SystemPrompt,
		SessionID:    opts.SessionID,
		UserID:       userID,
		AgentID:      agentID,
	})

	return &Agent{
		id:          id,
		userID:      userID,
		agentID:     agentID,
		state:       state,
		subscribers: make(map[string]chan AgentEvent),
		lane:        NewLane(),
	}
}

// Subscribe registers a new event channel and returns its subscription ID.
func (a *Agent) Subscribe() (string, chan AgentEvent) {
	subID := uuid.New().String()
	ch := make(chan AgentEvent, defaultEventChannelBuffer)

	a.subscribersMu.Lock()
	a.subscribers[subID] = ch
	a.subscribersMu.Unlock()

	return subID, ch
}

// Unsubscribe removes the event channel for the given subscription ID.
func (a *Agent) Unsubscribe(id string) {
	a.subscribersMu.Lock()
	if ch, exists := a.subscribers[id]; exists {
		close(ch)
		delete(a.subscribers, id)
	}
	a.subscribersMu.Unlock()
}

// State returns a copy of the current agent state.
func (a *Agent) State() AgentState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Return a deep copy to prevent external mutation
	messages := make([]AgentMessage, len(a.state.Messages))
	copy(messages, a.state.Messages)

	pendingToolCalls := make(map[string]bool, len(a.state.PendingToolCalls))
	for k, v := range a.state.PendingToolCalls {
		pendingToolCalls[k] = v
	}

	tools := make([]Tool, len(a.state.Tools))
	copy(tools, a.state.Tools)

	return AgentState{
		SystemPrompt:     a.state.SystemPrompt,
		Model:            a.state.Model,
		ThinkingLevel:    a.state.ThinkingLevel,
		Tools:            tools,
		Messages:         messages,
		IsStreaming:      a.state.IsStreaming,
		StreamMessage:    a.state.StreamMessage,
		PendingToolCalls: pendingToolCalls,
		Error:            a.state.Error,
		SessionID:        a.state.SessionID,
		UserID:           a.state.UserID,
		AgentID:          a.state.AgentID,
	}
}

// Lane returns the agent's message queue lane.
func (a *Agent) Lane() *Lane {
	return a.lane
}

// AddTools adds tools to the agent's tool list in a thread-safe manner.
// This is used for dynamic tool registration, such as when nodes connect.
func (a *Agent) AddTools(tools ...Tool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.Tools = append(a.state.Tools, tools...)
}

// RemoveTools removes tools from the agent by name prefix.
// This is used when nodes disconnect.
func (a *Agent) RemoveTools(prefix string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var updated []Tool
	for _, tool := range a.state.Tools {
		// Keep tools that don't start with the prefix
		if len(tool.Name) < len(prefix) || tool.Name[:len(prefix)] != prefix {
			updated = append(updated, tool)
		}
	}
	a.state.Tools = updated
}
