package agent_test

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "4 chars = 1 token",
			text:     "test",
			expected: 1,
		},
		{
			name:     "8 chars = 2 tokens",
			text:     "testtest",
			expected: 2,
		},
		{
			name:     "5 chars = 2 tokens (rounded up)",
			text:     "tests",
			expected: 2,
		},
		{
			name:     "100 chars = 25 tokens",
			text:     string(make([]byte, 100)),
			expected: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agent.EstimateTokens(tt.text)
			if result != tt.expected {
				t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, result, tt.expected)
			}
		})
	}
}

func TestAssembleContext(t *testing.T) {
	t.Run("assembles messages without compaction when under threshold", func(t *testing.T) {
		ctx := context.Background()
		mockStore := agent.NewMockHistoryStore()
		assembler := agent.NewContextAssembler(mockStore, 1000)

		history := []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Hello"),
			agent.NewTextMessage(agent.RoleAssistant, "Hi there!"),
		}
		mockStore.SetHistory("session-1", history)

		newMessages := []string{"How are you?"}

		result, err := assembler.AssembleContext(ctx, "session-1", newMessages)
		if err != nil {
			t.Fatalf("AssembleContext returned error: %v", err)
		}

		if len(result) != 3 {
			t.Errorf("expected 3 messages, got %d", len(result))
		}

		lastMsg := result[len(result)-1]
		if lastMsg.Role != agent.RoleUser {
			t.Errorf("expected last message role to be user, got %v", lastMsg.Role)
		}
	})

	t.Run("compacts history when over threshold", func(t *testing.T) {
		ctx := context.Background()
		mockStore := agent.NewMockHistoryStore()
		assembler := agent.NewContextAssembler(mockStore, 400)

		var history []agent.AgentMessage
		for i := 0; i < 30; i++ {
			history = append(history,
				agent.NewTextMessage(agent.RoleUser, "This is a longer message to add tokens"),
				agent.NewTextMessage(agent.RoleAssistant, "This is a longer response to add more tokens"),
			)
		}
		mockStore.SetHistory("session-1", history)

		newMessages := []string{"New message"}

		result, err := assembler.AssembleContext(ctx, "session-1", newMessages)
		if err != nil {
			t.Fatalf("AssembleContext returned error: %v", err)
		}

		if len(result) > 25 {
			t.Errorf("expected history to be compacted to <= 25 messages, got %d", len(result))
		}

		hasSummary := false
		for _, msg := range result {
			for _, block := range msg.Content {
				if tc, ok := block.(agent.TextContent); ok {
					if len(tc.Text) > 0 && tc.Text[0] == '[' {
						hasSummary = true
					}
				}
			}
		}
		if !hasSummary {
			t.Error("expected a summary message in compacted history")
		}
	})
}

func TestCompactHistory(t *testing.T) {
	t.Run("keeps last 20 turns and summarizes rest", func(t *testing.T) {
		mockStore := agent.NewMockHistoryStore()
		assembler := agent.NewContextAssembler(mockStore, 10000)

		var history []agent.AgentMessage
		for i := 0; i < 50; i++ {
			history = append(history,
				agent.NewTextMessage(agent.RoleUser, "Message "+string(rune('0'+i%10))),
				agent.NewTextMessage(agent.RoleAssistant, "Response "+string(rune('0'+i%10))),
			)
		}

		compacted := assembler.CompactHistory(history)

		if len(compacted) > 22 {
			t.Errorf("expected at most 22 messages (1 summary + 20 turns + 1 extra), got %d", len(compacted))
		}

		for i := 1; i < len(compacted); i++ {
			if compacted[i].Role != agent.RoleUser && compacted[i].Role != agent.RoleAssistant {
				t.Errorf("unexpected role at position %d: %v", i, compacted[i].Role)
			}
		}
	})

	t.Run("does not compact small history", func(t *testing.T) {
		mockStore := agent.NewMockHistoryStore()
		assembler := agent.NewContextAssembler(mockStore, 10000)

		history := []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Hello"),
			agent.NewTextMessage(agent.RoleAssistant, "Hi"),
			agent.NewTextMessage(agent.RoleUser, "How are you?"),
		}

		compacted := assembler.CompactHistory(history)

		if len(compacted) != 3 {
			t.Errorf("expected 3 messages, got %d", len(compacted))
		}
	})
}

func TestHistoryStore(t *testing.T) {
	t.Run("save and load history", func(t *testing.T) {
		ctx := context.Background()
		store := agent.NewMockHistoryStore()

		history := []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Hello"),
			agent.NewTextMessage(agent.RoleAssistant, "Hi there!"),
		}

		err := store.SaveHistory(ctx, "session-1", history)
		if err != nil {
			t.Fatalf("SaveHistory returned error: %v", err)
		}

		loaded, err := store.LoadHistory(ctx, "session-1")
		if err != nil {
			t.Fatalf("LoadHistory returned error: %v", err)
		}

		if len(loaded) != 2 {
			t.Errorf("expected 2 messages, got %d", len(loaded))
		}
	})

	t.Run("returns empty slice for unknown session", func(t *testing.T) {
		ctx := context.Background()
		store := agent.NewMockHistoryStore()

		loaded, err := store.LoadHistory(ctx, "unknown-session")
		if err != nil {
			t.Fatalf("LoadHistory returned error: %v", err)
		}

		if len(loaded) != 0 {
			t.Errorf("expected 0 messages, got %d", len(loaded))
		}
	})
}
