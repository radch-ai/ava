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
		{
			name:  "TaskQueuedEvent",
			event: agent.TaskQueuedEvent{},
		},
		{
			name:  "TaskProcessingEvent",
			event: agent.TaskProcessingEvent{},
		},
		{
			name:  "TaskCondensedEvent",
			event: agent.TaskCondensedEvent{},
		},
		{
			name:  "TaskSteeredEvent",
			event: agent.TaskSteeredEvent{},
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

// TestTaskQueuedEvent verifies TaskQueuedEvent has MessageID and Position fields.
func TestTaskQueuedEvent(t *testing.T) {
	evt := agent.TaskQueuedEvent{
		MessageID: "msg-123",
		Position:  5,
	}

	if evt.MessageID != "msg-123" {
		t.Errorf("expected MessageID 'msg-123', got %s", evt.MessageID)
	}
	if evt.Position != 5 {
		t.Errorf("expected Position 5, got %d", evt.Position)
	}
}

// TestTaskProcessingEvent verifies TaskProcessingEvent has MessageID, RunID, and SessionID fields.
func TestTaskProcessingEvent(t *testing.T) {
	evt := agent.TaskProcessingEvent{
		MessageID: "msg-456",
		RunID:     "run-789",
		SessionID: "session-abc",
	}

	if evt.MessageID != "msg-456" {
		t.Errorf("expected MessageID 'msg-456', got %s", evt.MessageID)
	}
	if evt.RunID != "run-789" {
		t.Errorf("expected RunID 'run-789', got %s", evt.RunID)
	}
	if evt.SessionID != "session-abc" {
		t.Errorf("expected SessionID 'session-abc', got %s", evt.SessionID)
	}
}

// TestTaskCondensedEvent verifies TaskCondensedEvent has OriginalCount and CondensedCount fields.
func TestTaskCondensedEvent(t *testing.T) {
	evt := agent.TaskCondensedEvent{
		OriginalCount:  10,
		CondensedCount: 3,
	}

	if evt.OriginalCount != 10 {
		t.Errorf("expected OriginalCount 10, got %d", evt.OriginalCount)
	}
	if evt.CondensedCount != 3 {
		t.Errorf("expected CondensedCount 3, got %d", evt.CondensedCount)
	}
}

// TestTaskSteeredEvent verifies TaskSteeredEvent has CancelledCount field.
func TestTaskSteeredEvent(t *testing.T) {
	evt := agent.TaskSteeredEvent{
		CancelledCount: 7,
	}

	if evt.CancelledCount != 7 {
		t.Errorf("expected CancelledCount 7, got %d", evt.CancelledCount)
	}
}
