// Package agent provides core types and events for the agent runtime.
//
// # Events
//
// Events are emitted during agent execution to provide real-time visibility into:
// - Agent lifecycle (start, end)
// - Turn lifecycle (turn start, turn end)
// - Message generation (start, update, end)
// - Tool execution (start, update, end)
//
// Subscribe to events using Agent.Subscribe():
//
//	subID, ch := agent.Subscribe()
//	for event := range ch {
//	    switch e := event.(type) {
//	    case AgentStartEvent:
//	        log.Printf("Agent started at %d", e.Timestamp)
//	    case ToolExecutionEndEvent:
//	        log.Printf("Tool %s completed: %v", e.ToolName, e.Result)
//	    }
//	}
package agent

// AgentEvent is a marker interface for all agent event types.
type AgentEvent interface {
	isAgentEvent()
}

// AgentStartEvent is emitted when an agent starts processing.
type AgentStartEvent struct {
	Timestamp int64
}

func (AgentStartEvent) isAgentEvent() {}

// AgentEndEvent is emitted when an agent finishes processing.
type AgentEndEvent struct {
	Timestamp int64
	Messages  []AgentMessage
}

func (AgentEndEvent) isAgentEvent() {}

// TurnStartEvent is emitted when a new turn (interaction cycle) begins.
type TurnStartEvent struct {
	Timestamp int64
}

func (TurnStartEvent) isAgentEvent() {}

// TurnEndEvent is emitted when a turn completes.
type TurnEndEvent struct {
	Timestamp   int64
	Message     AgentMessage
	ToolResults []AgentMessage
}

func (TurnEndEvent) isAgentEvent() {}

// MessageStartEvent is emitted when a message starts being generated.
type MessageStartEvent struct {
	Timestamp int64
	Message   AgentMessage
}

func (MessageStartEvent) isAgentEvent() {}

// MessageUpdateEvent is emitted when streaming content is added to a message.
type MessageUpdateEvent struct {
	Timestamp int64
	Message   AgentMessage
	Delta     string
}

func (MessageUpdateEvent) isAgentEvent() {}

// MessageEndEvent is emitted when a message is complete.
type MessageEndEvent struct {
	Timestamp int64
	Message   AgentMessage
}

func (MessageEndEvent) isAgentEvent() {}

// ToolExecutionStartEvent is emitted when a tool execution begins.
type ToolExecutionStartEvent struct {
	Timestamp  int64
	ToolCallID string
	ToolName   string
	Args       map[string]any
}

func (ToolExecutionStartEvent) isAgentEvent() {}

// ToolExecutionUpdateEvent is emitted when a tool produces partial results during execution.
type ToolExecutionUpdateEvent struct {
	Timestamp     int64
	ToolCallID    string
	ToolName      string
	PartialResult any
}

func (ToolExecutionUpdateEvent) isAgentEvent() {}

// ToolExecutionEndEvent is emitted when a tool execution completes.
type ToolExecutionEndEvent struct {
	Timestamp  int64
	ToolCallID string
	ToolName   string
	Result     any
	IsError    bool
}

func (ToolExecutionEndEvent) isAgentEvent() {}

// TokenUsageEvent is emitted when token usage information is available.
type TokenUsageEvent struct {
	Timestamp    int64
	TokensInput  int64
	TokensOutput int64
}

func (TokenUsageEvent) isAgentEvent() {}
