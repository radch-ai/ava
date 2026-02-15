// Package agent_test provides tests for the agent runtime.
package agent_test

import (
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// TestEventInterface validates that all event types implement the AgentEvent interface.
func TestEventInterface(t *testing.T) {
	tests := []struct {
		name  string
		event agent.AgentEvent
	}{
		{
			name:  "AgentStartEvent",
			event: agent.AgentStartEvent{},
		},
		{
			name:  "AgentEndEvent",
			event: agent.AgentEndEvent{},
		},
		{
			name:  "TurnStartEvent",
			event: agent.TurnStartEvent{},
		},
		{
			name:  "TurnEndEvent",
			event: agent.TurnEndEvent{},
		},
		{
			name:  "MessageStartEvent",
			event: agent.MessageStartEvent{},
		},
		{
			name:  "MessageUpdateEvent",
			event: agent.MessageUpdateEvent{},
		},
		{
			name:  "MessageEndEvent",
			event: agent.MessageEndEvent{},
		},
		{
			name:  "ToolExecutionStartEvent",
			event: agent.ToolExecutionStartEvent{},
		},
		{
			name:  "ToolExecutionUpdateEvent",
			event: agent.ToolExecutionUpdateEvent{},
		},
		{
			name:  "ToolExecutionEndEvent",
			event: agent.ToolExecutionEndEvent{},
		},
		{
			name:  "TokenUsageEvent",
			event: agent.TokenUsageEvent{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// If this compiles, the event implements AgentEvent
			_ = agent.AgentEvent(tt.event)
		})
	}
}

// TestEventTimestamps verifies that events have proper timestamp handling.
func TestEventTimestamps(t *testing.T) {
	now := time.Now().Unix()

	events := []agent.AgentEvent{
		agent.AgentStartEvent{Timestamp: now},
		agent.TurnStartEvent{Timestamp: now},
		agent.MessageStartEvent{Timestamp: now},
	}

	for i, evt := range events {
		switch e := evt.(type) {
		case agent.AgentStartEvent:
			if e.Timestamp != now {
				t.Errorf("Event %d: expected timestamp %d, got %d", i, now, e.Timestamp)
			}
		case agent.TurnStartEvent:
			if e.Timestamp != now {
				t.Errorf("Event %d: expected timestamp %d, got %d", i, now, e.Timestamp)
			}
		case agent.MessageStartEvent:
			if e.Timestamp != now {
				t.Errorf("Event %d: expected timestamp %d, got %d", i, now, e.Timestamp)
			}
		}
	}
}

// TestAgentEndEventMessages verifies that AgentEndEvent contains messages.
func TestAgentEndEventMessages(t *testing.T) {
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, "test"),
	}

	evt := agent.AgentEndEvent{
		Messages: messages,
	}

	if len(evt.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(evt.Messages))
	}
}
