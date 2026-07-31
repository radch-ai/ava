// Package agent_test provides tests for the agent runtime.
package agent_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
)

func TestRoleValues(t *testing.T) {
	tests := []struct {
		name  string
		role  agent.Role
		valid bool
	}{
		{"user role", agent.RoleUser, true},
		{"assistant role", agent.RoleAssistant, true},
		{"tool result role", agent.RoleToolResult, true},
		{"invalid role", agent.Role("invalid"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.role {
			case agent.RoleUser, agent.RoleAssistant, agent.RoleToolResult:
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
		role agent.Role
		want string
	}{
		{agent.RoleUser, "user"},
		{agent.RoleAssistant, "assistant"},
		{agent.RoleToolResult, "toolResult"},
	}

	for _, tt := range tests {
		t.Run(tt.role.String(), func(t *testing.T) {
			if got := tt.role.String(); got != tt.want {
				t.Errorf("Role.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTextContent(t *testing.T) {
	t.Run("creates text content block", func(t *testing.T) {
		content := agent.TextContent{Text: "Hello, world!"}

		if content.Text != "Hello, world!" {
			t.Errorf("expected text 'Hello, world!', got %q", content.Text)
		}

		// Verify it implements ContentBlock
		var _ agent.ContentBlock = content
	})
}

func TestImageContent(t *testing.T) {
	t.Run("creates image content block", func(t *testing.T) {
		content := agent.ImageContent{
			Data:     "base64data",
			MimeType: "image/png",
		}

		if content.Data != "base64data" {
			t.Errorf("expected Data 'base64data', got %q", content.Data)
		}

		if content.MimeType != "image/png" {
			t.Errorf("expected MimeType 'image/png', got %q", content.MimeType)
		}

		// Verify it implements ContentBlock
		var _ agent.ContentBlock = content
	})
}

func TestToolCallContent(t *testing.T) {
	t.Run("creates tool call content block", func(t *testing.T) {
		args := map[string]any{
			"param1": "value1",
			"param2": 42,
		}

		content := agent.ToolCallContent{
			ID:        "call_123",
			Name:      "my_tool",
			Arguments: args,
		}

		if content.ID != "call_123" {
			t.Errorf("expected ID 'call_123', got %q", content.ID)
		}

		if content.Name != "my_tool" {
			t.Errorf("expected Name 'my_tool', got %q", content.Name)
		}

		if len(content.Arguments) != 2 {
			t.Errorf("expected 2 arguments, got %d", len(content.Arguments))
		}

		// Verify it implements ContentBlock
		var _ agent.ContentBlock = content
	})
}

func TestAgentMessage(t *testing.T) {
	t.Run("creates agent message with text content", func(t *testing.T) {
		msg := agent.AgentMessage{
			Role:      agent.RoleUser,
			Timestamp: time.Now().Unix(),
			Content: []agent.ContentBlock{
				agent.TextContent{Text: "Test message"},
			},
		}

		if msg.Role != agent.RoleUser {
			t.Errorf("expected role %v, got %v", agent.RoleUser, msg.Role)
		}

		if len(msg.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(msg.Content))
		}

		text, ok := msg.Content[0].(agent.TextContent)
		if !ok {
			t.Fatalf("expected TextContent, got %T", msg.Content[0])
		}

		if text.Text != "Test message" {
			t.Errorf("expected text 'Test message', got %q", text.Text)
		}
	})
}

func TestNewTextMessage(t *testing.T) {
	t.Run("creates new text message", func(t *testing.T) {
		msg := agent.NewTextMessage(agent.RoleUser, "Hello!")

		if msg.Role != agent.RoleUser {
			t.Errorf("expected role %v, got %v", agent.RoleUser, msg.Role)
		}

		if len(msg.Content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(msg.Content))
		}

		if msg.Timestamp == 0 {
			t.Error("expected timestamp to be set")
		}
	})
}

func TestContentBlockJSON(t *testing.T) {
	t.Run("marshals text content to JSON", func(t *testing.T) {
		content := agent.TextContent{Text: "Test"}
		data, err := json.Marshal(content)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}

		var got map[string]any
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if got["type"] != "text" {
			t.Errorf("expected type 'text', got %v", got["type"])
		}

		if got["text"] != "Test" {
			t.Errorf("expected text 'Test', got %v", got["text"])
		}
	})
}

// TestAgentStateWithUserID verifies AgentState includes UserID
func TestAgentStateWithUserID(t *testing.T) {
	opts := agent.AgentStateOptions{
		UserID:    "user-123",
		AgentID:   "agent-456",
		SessionID: "session-789",
	}
	state := agent.NewAgentState(opts)

	if state.UserID != "user-123" {
		t.Errorf("expected UserID user-123, got %s", state.UserID)
	}
	if state.AgentID != "agent-456" {
		t.Errorf("expected AgentID agent-456, got %s", state.AgentID)
	}
}

// TestAgentWithUserID verifies Agent includes UserID
func TestAgentWithUserID(t *testing.T) {
	opts := agent.AgentOptions{
		UserID:  "user-123",
		AgentID: "agent-456",
	}
	a := agent.NewAgent(opts)

	state := a.State()
	if state.UserID != "user-123" {
		t.Errorf("expected UserID user-123, got %s", state.UserID)
	}
	if state.AgentID != "agent-456" {
		t.Errorf("expected AgentID agent-456, got %s", state.AgentID)
	}
}
