package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRoleValues(t *testing.T) {
	tests := []struct {
		name  string
		role  Role
		valid bool
	}{
		{"user role", RoleUser, true},
		{"assistant role", RoleAssistant, true},
		{"tool result role", RoleToolResult, true},
		{"invalid role", Role("invalid"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.role {
			case RoleUser, RoleAssistant, RoleToolResult:
				if !tt.valid {
					t.Errorf("Role %q should be valid", tt.role)
				}
			default:
				if tt.valid {
					t.Errorf("Role %q should be invalid", tt.role)
				}
			}
		})
	}
}

func TestRoleString(t *testing.T) {
	tests := []struct {
		role Role
		want string
	}{
		{RoleUser, "user"},
		{RoleAssistant, "assistant"},
		{RoleToolResult, "toolResult"},
	}

	for _, tt := range tests {
		t.Run(tt.role.String(), func(t *testing.T) {
			if got := tt.role.String(); got != tt.want {
				t.Errorf("Role.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContentBlockTypes(t *testing.T) {
	tests := []struct {
		name       string
		block      ContentBlock
		isText     bool
		isImage    bool
		isToolCall bool
	}{
		{
			name:   "text content",
			block:  TextContent{Text: "hello"},
			isText: true,
		},
		{
			name: "image content",
			block: ImageContent{
				Data:     "base64data",
				MimeType: "image/png",
			},
			isImage: true,
		},
		{
			name: "tool call content",
			block: ToolCallContent{
				ID:   "call_123",
				Name: "web_search",
				Arguments: map[string]any{
					"query": "golang",
				},
			},
			isToolCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch v := tt.block.(type) {
			case TextContent:
				if !tt.isText {
					t.Errorf("expected TextContent, got %T", v)
				}
			case ImageContent:
				if !tt.isImage {
					t.Errorf("expected ImageContent, got %T", v)
				}
			case ToolCallContent:
				if !tt.isToolCall {
					t.Errorf("expected ToolCallContent, got %T", v)
				}
			default:
				t.Errorf("unknown ContentBlock type: %T", v)
			}
		})
	}
}

func TestNewTextMessage(t *testing.T) {
	msg := NewTextMessage(RoleUser, "Hello, world!")

	if msg.Role != RoleUser {
		t.Errorf("expected RoleUser, got %v", msg.Role)
	}

	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}

	text, ok := msg.Content[0].(TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", msg.Content[0])
	}

	if text.Text != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", text.Text)
	}

	if msg.Timestamp == 0 {
		t.Error("expected timestamp to be set")
	}
}

func TestNewToolCallMessage(t *testing.T) {
	msg := NewToolCallMessage("call_abc", "web_search", map[string]any{
		"query": "golang tutorials",
	})

	if msg.Role != RoleAssistant {
		t.Errorf("expected RoleAssistant, got %v", msg.Role)
	}

	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}

	tc, ok := msg.Content[0].(ToolCallContent)
	if !ok {
		t.Fatalf("expected ToolCallContent, got %T", msg.Content[0])
	}

	if tc.ID != "call_abc" {
		t.Errorf("expected ID 'call_abc', got %q", tc.ID)
	}

	if tc.Name != "web_search" {
		t.Errorf("expected Name 'web_search', got %q", tc.Name)
	}

	if tc.Arguments["query"] != "golang tutorials" {
		t.Errorf("unexpected arguments: %v", tc.Arguments)
	}
}

func TestNewToolResultMessage(t *testing.T) {
	msg := NewToolResultMessage("call_abc", "web_search", "Found 5 results", false)

	if msg.Role != RoleToolResult {
		t.Errorf("expected RoleToolResult, got %v", msg.Role)
	}

	if msg.ToolCallID != "call_abc" {
		t.Errorf("expected ToolCallID 'call_abc', got %q", msg.ToolCallID)
	}

	if msg.ToolName != "web_search" {
		t.Errorf("expected ToolName 'web_search', got %q", msg.ToolName)
	}

	if msg.IsError {
		t.Error("expected IsError to be false")
	}

	if len(msg.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(msg.Content))
	}

	text, ok := msg.Content[0].(TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", msg.Content[0])
	}

	if text.Text != "Found 5 results" {
		t.Errorf("expected 'Found 5 results', got %q", text.Text)
	}
}

func TestNewToolResultMessageError(t *testing.T) {
	msg := NewToolResultMessage("call_abc", "web_search", "API error", true)

	if !msg.IsError {
		t.Error("expected IsError to be true")
	}
}

func TestAgentMessageWithMultipleContentBlocks(t *testing.T) {
	now := time.Now()
	msg := AgentMessage{
		Role:      RoleUser,
		Timestamp: now.Unix(),
		Content: []ContentBlock{
			TextContent{Text: "Search for this:"},
			ImageContent{
				Data:     "iVBORw0KGgo...",
				MimeType: "image/png",
			},
		},
	}

	if len(msg.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(msg.Content))
	}

	if _, ok := msg.Content[0].(TextContent); !ok {
		t.Error("first block should be TextContent")
	}

	if _, ok := msg.Content[1].(ImageContent); !ok {
		t.Error("second block should be ImageContent")
	}
}

func TestNewAgentState(t *testing.T) {
	t.Run("initializes with provided options", func(t *testing.T) {
		opts := AgentStateOptions{
			SystemPrompt: "You are a helpful assistant.",
			Model: Model{
				ID:       "claude-3-opus-20240229",
				API:      "anthropic",
				Provider: "Anthropic",
				BaseURL:  "https://api.anthropic.com",
			},
			ThinkingLevel: ThinkingLevelHigh,
			Tools: []Tool{
				{
					Name:        "test_tool",
					Description: "A test tool",
				},
			},
			SessionID: "session-123",
		}

		state := NewAgentState(opts)

		if state.SystemPrompt != opts.SystemPrompt {
			t.Errorf("SystemPrompt = %q, want %q", state.SystemPrompt, opts.SystemPrompt)
		}

		if state.Model.ID != opts.Model.ID {
			t.Errorf("Model.ID = %q, want %q", state.Model.ID, opts.Model.ID)
		}

		if state.ThinkingLevel != ThinkingLevelHigh {
			t.Errorf("ThinkingLevel = %v, want %v", state.ThinkingLevel, ThinkingLevelHigh)
		}

		if len(state.Tools) != 1 {
			t.Errorf("Tools length = %d, want 1", len(state.Tools))
		}

		if state.SessionID != "session-123" {
			t.Errorf("SessionID = %q, want 'session-123'", state.SessionID)
		}
	})

	t.Run("initializes with empty defaults", func(t *testing.T) {
		opts := AgentStateOptions{}
		state := NewAgentState(opts)

		if state.Messages == nil {
			t.Error("Messages should be initialized to empty slice, not nil")
		}

		if len(state.Messages) != 0 {
			t.Errorf("Messages length = %d, want 0", len(state.Messages))
		}

		if state.PendingToolCalls == nil {
			t.Error("PendingToolCalls should be initialized, not nil")
		}

		if state.IsStreaming {
			t.Error("IsStreaming should be false initially")
		}

		if state.StreamMessage != nil {
			t.Error("StreamMessage should be nil initially")
		}

		if state.Error != "" {
			t.Errorf("Error = %q, want empty string", state.Error)
		}
	})
}

func TestAgentStateAddMessage(t *testing.T) {
	t.Run("adds message to history", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{})

		msg := NewTextMessage(RoleUser, "Hello")
		state.AddMessage(msg)

		if len(state.Messages) != 1 {
			t.Fatalf("Messages length = %d, want 1", len(state.Messages))
		}

		if state.Messages[0].Role != RoleUser {
			t.Errorf("Messages[0].Role = %v, want %v", state.Messages[0].Role, RoleUser)
		}
	})

	t.Run("appends multiple messages", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{})

		state.AddMessage(NewTextMessage(RoleUser, "Hello"))
		state.AddMessage(NewTextMessage(RoleAssistant, "Hi there"))
		state.AddMessage(NewTextMessage(RoleUser, "How are you?"))

		if len(state.Messages) != 3 {
			t.Fatalf("Messages length = %d, want 3", len(state.Messages))
		}

		if state.Messages[2].Role != RoleUser {
			t.Errorf("Messages[2].Role = %v, want %v", state.Messages[2].Role, RoleUser)
		}
	})
}

func TestAgentStateSetStreaming(t *testing.T) {
	t.Run("sets streaming state", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{})

		msg := NewTextMessage(RoleAssistant, "thinking...")
		state.SetStreaming(true, &msg)

		if !state.IsStreaming {
			t.Error("IsStreaming should be true")
		}

		if state.StreamMessage == nil {
			t.Error("StreamMessage should be set")
		}

		if state.StreamMessage.Role != RoleAssistant {
			t.Errorf("StreamMessage.Role = %v, want %v", state.StreamMessage.Role, RoleAssistant)
		}
	})

	t.Run("clears streaming state", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{})

		msg := NewTextMessage(RoleAssistant, "done")
		state.SetStreaming(true, &msg)
		state.SetStreaming(false, nil)

		if state.IsStreaming {
			t.Error("IsStreaming should be false")
		}

		if state.StreamMessage != nil {
			t.Error("StreamMessage should be nil")
		}
	})

	t.Run("sets streaming without message", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{})

		state.SetStreaming(true, nil)

		if !state.IsStreaming {
			t.Error("IsStreaming should be true")
		}

		if state.StreamMessage != nil {
			t.Error("StreamMessage should be nil when not provided")
		}
	})
}

func TestAgentStatePendingToolCalls(t *testing.T) {
	t.Run("tracks pending tool calls", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{})

		state.AddPendingToolCall("call_1")
		state.AddPendingToolCall("call_2")
		state.AddPendingToolCall("call_3")

		if !state.HasPendingToolCall("call_1") {
			t.Error("call_1 should be pending")
		}

		if !state.HasPendingToolCall("call_2") {
			t.Error("call_2 should be pending")
		}

		if !state.HasPendingToolCall("call_3") {
			t.Error("call_3 should be pending")
		}

		if state.HasPendingToolCall("call_4") {
			t.Error("call_4 should not be pending")
		}
	})

	t.Run("removes pending tool calls", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{})

		state.AddPendingToolCall("call_1")
		state.AddPendingToolCall("call_2")

		state.RemovePendingToolCall("call_1")

		if state.HasPendingToolCall("call_1") {
			t.Error("call_1 should not be pending after removal")
		}

		if !state.HasPendingToolCall("call_2") {
			t.Error("call_2 should still be pending")
		}
	})

	t.Run("handles removing non-existent calls", func(t *testing.T) {
		state := NewAgentState(AgentStateOptions{})

		// Should not panic
		state.RemovePendingToolCall("nonexistent")

		if state.HasPendingToolCall("nonexistent") {
			t.Error("nonexistent should not be pending")
		}
	})
}

func TestAgentMessageJSON(t *testing.T) {
	t.Run("marshals and unmarshals text message", func(t *testing.T) {
		msg := NewTextMessage(RoleUser, "Hello, world!")

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded AgentMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if decoded.Role != msg.Role {
			t.Errorf("expected Role %v, got %v", msg.Role, decoded.Role)
		}

		if len(decoded.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(decoded.Content))
		}

		text, ok := decoded.Content[0].(TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", decoded.Content[0])
		}

		if text.Text != "Hello, world!" {
			t.Errorf("expected 'Hello, world!', got %q", text.Text)
		}
	})

	t.Run("marshals and unmarshals tool call message", func(t *testing.T) {
		msg := NewToolCallMessage("call_123", "web_search", map[string]any{
			"query": "golang",
			"count": 5,
		})

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded AgentMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		tc, ok := decoded.Content[0].(ToolCallContent)
		if !ok {
			t.Fatalf("expected ToolCallContent, got %T", decoded.Content[0])
		}

		if tc.ID != "call_123" {
			t.Errorf("expected ID 'call_123', got %q", tc.ID)
		}

		if tc.Name != "web_search" {
			t.Errorf("expected Name 'web_search', got %q", tc.Name)
		}

		if tc.Arguments["query"] != "golang" {
			t.Errorf("expected query 'golang', got %v", tc.Arguments["query"])
		}
	})

	t.Run("marshals and unmarshals image content", func(t *testing.T) {
		msg := AgentMessage{
			Role:      RoleUser,
			Timestamp: time.Now().Unix(),
			Content: []ContentBlock{
				ImageContent{
					Data:     "iVBORw0KGgo...",
					MimeType: "image/png",
				},
			},
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded AgentMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		img, ok := decoded.Content[0].(ImageContent)
		if !ok {
			t.Fatalf("expected ImageContent, got %T", decoded.Content[0])
		}

		if img.Data != "iVBORw0KGgo..." {
			t.Errorf("unexpected data: %s", img.Data)
		}

		if img.MimeType != "image/png" {
			t.Errorf("expected MIME type 'image/png', got %s", img.MimeType)
		}
	})

	t.Run("marshals and unmarshals tool result message", func(t *testing.T) {
		msg := NewToolResultMessage("call_123", "web_search", "Results found", false)

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded AgentMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if decoded.Role != RoleToolResult {
			t.Errorf("expected RoleToolResult, got %v", decoded.Role)
		}

		if decoded.ToolCallID != "call_123" {
			t.Errorf("expected ToolCallID 'call_123', got %q", decoded.ToolCallID)
		}

		if decoded.ToolName != "web_search" {
			t.Errorf("expected ToolName 'web_search', got %q", decoded.ToolName)
		}

		if decoded.IsError {
			t.Error("expected IsError to be false")
		}
	})

	t.Run("marshals and unmarshals mixed content blocks", func(t *testing.T) {
		msg := AgentMessage{
			Role:      RoleUser,
			Timestamp: time.Now().Unix(),
			Content: []ContentBlock{
				TextContent{Text: "Look at this image:"},
				ImageContent{
					Data:     "base64data",
					MimeType: "image/jpeg",
				},
			},
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var decoded AgentMessage
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if len(decoded.Content) != 2 {
			t.Fatalf("expected 2 content blocks, got %d", len(decoded.Content))
		}

		_, okText := decoded.Content[0].(TextContent)
		if !okText {
			t.Error("first block should be TextContent")
		}

		_, okImage := decoded.Content[1].(ImageContent)
		if !okImage {
			t.Error("second block should be ImageContent")
		}
	})

	t.Run("returns error for unknown content block type", func(t *testing.T) {
		jsonData := `{
			"role": "user",
			"timestamp": 1234567890,
			"content": [{"type": "unknown_type", "data": "value"}]
		}`

		var decoded AgentMessage
		err := json.Unmarshal([]byte(jsonData), &decoded)

		if err == nil {
			t.Error("expected error for unknown content block type, got nil")
		}
	})
}
