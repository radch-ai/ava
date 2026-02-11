// Package agent provides core types and events for the agent runtime.
package agent

import (
	"testing"
	"time"
)

// TestEventInterface validates that all event types implement the AgentEvent interface.
func TestEventInterface(t *testing.T) {
	tests := []struct {
		name  string
		event AgentEvent
	}{
		{
			name:  "AgentStartEvent",
			event: AgentStartEvent{},
		},
		{
			name:  "AgentEndEvent",
			event: AgentEndEvent{},
		},
		{
			name:  "TurnStartEvent",
			event: TurnStartEvent{},
		},
		{
			name:  "TurnEndEvent",
			event: TurnEndEvent{},
		},
		{
			name:  "MessageStartEvent",
			event: MessageStartEvent{},
		},
		{
			name:  "MessageUpdateEvent",
			event: MessageUpdateEvent{},
		},
		{
			name:  "MessageEndEvent",
			event: MessageEndEvent{},
		},
		{
			name:  "ToolExecutionStartEvent",
			event: ToolExecutionStartEvent{},
		},
		{
			name:  "ToolExecutionUpdateEvent",
			event: ToolExecutionUpdateEvent{},
		},
		{
			name:  "ToolExecutionEndEvent",
			event: ToolExecutionEndEvent{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// If the event type doesn't exist or doesn't implement AgentEvent,
			// this will fail to compile - which is the RED phase we want.
			_ = tt.event
		})
	}
}

// TestEventFields validates that event types have their required fields.
func TestEventFields(t *testing.T) {
	t.Run("AgentEndEvent_Messages", func(t *testing.T) {
		msg := NewTextMessage(RoleUser, "test message")
		event := AgentEndEvent{
			Messages: []AgentMessage{msg},
		}

		if len(event.Messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(event.Messages))
		}

		if event.Messages[0].Role != RoleUser {
			t.Errorf("expected RoleUser, got %v", event.Messages[0].Role)
		}
	})

	t.Run("TurnEndEvent_MessageAndToolResults", func(t *testing.T) {
		msg := NewTextMessage(RoleAssistant, "response")
		toolResult := NewToolResultMessage("call_123", "web_search", "results", false)

		event := TurnEndEvent{
			Message:     msg,
			ToolResults: []AgentMessage{toolResult},
		}

		if event.Message.Role != RoleAssistant {
			t.Errorf("expected RoleAssistant, got %v", event.Message.Role)
		}

		if len(event.ToolResults) != 1 {
			t.Fatalf("expected 1 tool result, got %d", len(event.ToolResults))
		}

		if event.ToolResults[0].Role != RoleToolResult {
			t.Errorf("expected RoleToolResult, got %v", event.ToolResults[0].Role)
		}
	})

	t.Run("MessageStartEvent_Message", func(t *testing.T) {
		msg := NewTextMessage(RoleAssistant, "starting")
		event := MessageStartEvent{
			Message: msg,
		}

		if event.Message.Role != RoleAssistant {
			t.Errorf("expected RoleAssistant, got %v", event.Message.Role)
		}
	})

	t.Run("MessageUpdateEvent_MessageAndDelta", func(t *testing.T) {
		msg := NewTextMessage(RoleAssistant, "initial")
		event := MessageUpdateEvent{
			Message: msg,
			Delta:   " additional",
		}

		if event.Delta != " additional" {
			t.Errorf("expected delta ' additional', got %q", event.Delta)
		}
	})

	t.Run("MessageEndEvent_Message", func(t *testing.T) {
		msg := NewTextMessage(RoleAssistant, "complete")
		event := MessageEndEvent{
			Message: msg,
		}

		if event.Message.Role != RoleAssistant {
			t.Errorf("expected RoleAssistant, got %v", event.Message.Role)
		}
	})

	t.Run("ToolExecutionStartEvent_Fields", func(t *testing.T) {
		args := map[string]any{
			"query": "golang events",
			"count": 5,
		}

		event := ToolExecutionStartEvent{
			ToolCallID: "call_abc123",
			ToolName:   "web_search",
			Args:       args,
		}

		if event.ToolCallID != "call_abc123" {
			t.Errorf("expected ToolCallID 'call_abc123', got %q", event.ToolCallID)
		}

		if event.ToolName != "web_search" {
			t.Errorf("expected ToolName 'web_search', got %q", event.ToolName)
		}

		if event.Args["query"] != "golang events" {
			t.Errorf("unexpected query: %v", event.Args["query"])
		}
	})

	t.Run("ToolExecutionUpdateEvent_Fields", func(t *testing.T) {
		partialResult := map[string]any{
			"results": []string{"item1", "item2"},
		}

		event := ToolExecutionUpdateEvent{
			ToolCallID:    "call_abc123",
			ToolName:      "web_search",
			PartialResult: partialResult,
		}

		if event.ToolCallID != "call_abc123" {
			t.Errorf("expected ToolCallID 'call_abc123', got %q", event.ToolCallID)
		}

		if event.ToolName != "web_search" {
			t.Errorf("expected ToolName 'web_search', got %q", event.ToolName)
		}

		results, ok := event.PartialResult.(map[string]any)
		if !ok {
			t.Fatal("expected PartialResult to be map[string]any")
		}

		if len(results["results"].([]string)) != 2 {
			t.Errorf("expected 2 results, got %d", len(results["results"].([]string)))
		}
	})

	t.Run("ToolExecutionEndEvent_Fields", func(t *testing.T) {
		result := map[string]any{
			"final": "output",
		}

		event := ToolExecutionEndEvent{
			ToolCallID: "call_abc123",
			ToolName:   "web_search",
			Result:     result,
			IsError:    false,
		}

		if event.ToolCallID != "call_abc123" {
			t.Errorf("expected ToolCallID 'call_abc123', got %q", event.ToolCallID)
		}

		if event.ToolName != "web_search" {
			t.Errorf("expected ToolName 'web_search', got %q", event.ToolName)
		}

		if event.IsError {
			t.Error("expected IsError to be false")
		}

		resultMap, ok := event.Result.(map[string]any)
		if !ok {
			t.Fatal("expected Result to be map[string]any")
		}

		if resultMap["final"] != "output" {
			t.Errorf("unexpected result: %v", resultMap["final"])
		}
	})

	t.Run("ToolExecutionEndEvent_IsError", func(t *testing.T) {
		event := ToolExecutionEndEvent{
			ToolCallID: "call_failed",
			ToolName:   "failing_tool",
			Result:     "error occurred",
			IsError:    true,
		}

		if !event.IsError {
			t.Error("expected IsError to be true")
		}

		if event.Result != "error occurred" {
			t.Errorf("expected result 'error occurred', got %v", event.Result)
		}
	})

	t.Run("AgentStartEvent_Timestamp", func(t *testing.T) {
		before := time.Now()
		event := AgentStartEvent{
			Timestamp: time.Now().Unix(),
		}
		after := time.Now()

		if event.Timestamp < before.Unix() || event.Timestamp > after.Unix() {
			t.Errorf("timestamp %d outside expected range [%d, %d]",
				event.Timestamp, before.Unix(), after.Unix())
		}
	})
}
