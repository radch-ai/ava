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
//
// # Prompting the Agent
//
// Send a user message to the agent:
//
//	ctx := context.Background()
//	err := agent.Prompt(ctx, "Hello, how are you?")
//	if err != nil {
//	    log.Fatal(err)
//	}
package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

const defaultEventChannelBuffer = 100

// AgentOptions configures a new Agent.
type AgentOptions struct {
	ID           string
	SystemPrompt string
	SessionID    string
	LLMClient    LLMClient // Optional LLM client for tool calling loop
}

// Agent manages agent state with thread-safe access and event broadcasting.
type Agent struct {
	mu            sync.RWMutex
	state         AgentState
	id            string
	subscribers   map[string]chan AgentEvent
	subscribersMu sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	llmClient     LLMClient // Optional LLM client for tool calling
}

// NewAgent creates a new Agent with the given options.
// If no ID is provided, a UUID will be generated.
func NewAgent(opts AgentOptions) *Agent {
	id := opts.ID
	if id == "" {
		id = uuid.New().String()
	}

	ctx, cancel := context.WithCancel(context.Background())

	state := NewAgentState(AgentStateOptions{
		SystemPrompt: opts.SystemPrompt,
		SessionID:    opts.SessionID,
	})

	return &Agent{
		id:          id,
		state:       state,
		subscribers: make(map[string]chan AgentEvent),
		ctx:         ctx,
		cancel:      cancel,
		llmClient:   opts.LLMClient,
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

// emit broadcasts an event to all subscribers.
func (a *Agent) emit(event AgentEvent) {
	a.subscribersMu.Lock()
	defer a.subscribersMu.Unlock()

	for _, ch := range a.subscribers {
		select {
		case ch <- event:
		default:
			// Channel full, skip this subscriber
		}
	}
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
	}
}

// Prompt sends a user message to the agent and runs the tool calling loop.
// This uses the Loop for actual LLM integration with streaming and tool execution.
func (a *Agent) Prompt(ctx context.Context, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Check if LLM client is configured
	if a.llmClient == nil {
		return fmt.Errorf("LLM client not configured")
	}

	// 1. Lock and add user message to state.Messages
	a.mu.Lock()
	userMsg := NewTextMessage(RoleUser, message)
	a.state.AddMessage(userMsg)
	a.mu.Unlock()

	// 2. Emit AgentStartEvent
	a.emit(AgentStartEvent{Timestamp: userMsg.Timestamp})

	// 3. Set streaming state
	a.setStreamingState(true, nil)

	// 4. Run tool calling loop using Loop with actual LLM integration
	// This replaces the old placeholder with real streaming + tool execution
	loop := NewLoop(a.llmClient)

	// Get current messages as starting point
	messages := a.State().Messages

	// Run loop - handles LLM streaming and tool execution
	result, err := loop.Run(ctx, messages, a.state.Tools...)
	if err != nil {
		a.setStreamingState(false, nil)
		return err
	}

	// 5. Update state with final messages from loop
	a.mu.Lock()
	a.state.Messages = result.Messages
	a.mu.Unlock()

	// 6. Emit AgentEndEvent
	finalState := a.State()
	a.emit(AgentEndEvent{
		Timestamp: finalState.Messages[len(finalState.Messages)-1].Timestamp,
		Messages:  finalState.Messages,
	})

	// 7. Clear streaming state
	a.setStreamingState(false, nil)

	return nil
}

// setStreamingState updates the streaming state under lock.
func (a *Agent) setStreamingState(streaming bool, msg *AgentMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.state.SetStreaming(streaming, msg)
}
