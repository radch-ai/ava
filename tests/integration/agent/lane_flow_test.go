//go:build integration
// +build integration

package agent_test

import (
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

func TestLane_FullFlow(t *testing.T) {
	t.Run("full lane flow with agent", func(t *testing.T) {
		opts := agent.AgentOptions{
			UserID:  "user-123",
			AgentID: "agent-456",
		}
		a := agent.NewAgent(opts)
		lane := a.Lane()

		if lane == nil {
			t.Fatal("agent.Lane() returned nil")
		}

		t.Run("initial queue depth is zero", func(t *testing.T) {
			status := lane.Status()
			if status.QueueDepth != 0 {
				t.Errorf("initial queue depth = %d, want 0", status.QueueDepth)
			}
		})

		t.Run("submit multiple messages", func(t *testing.T) {
			messages := []string{"hello", "world", "test"}
			var ids []string

			for _, msg := range messages {
				id := lane.Submit(msg)
				if id == "" {
					t.Errorf("Submit(%q) returned empty ID", msg)
				}
				ids = append(ids, id)
			}

			t.Run("verify queue depth", func(t *testing.T) {
				status := lane.Status()
				if status.QueueDepth != len(messages) {
					t.Errorf("queue depth = %d, want %d", status.QueueDepth, len(messages))
				}

				pending := lane.Pending()
				if len(pending) != len(messages) {
					t.Errorf("Pending() returned %d tasks, want %d", len(pending), len(messages))
				}

				for i, task := range pending {
					if task.Message != messages[i] {
						t.Errorf("task %d message = %q, want %q", i, task.Message, messages[i])
					}
					if task.ID != ids[i] {
						t.Errorf("task %d ID = %q, want %q", i, task.ID, ids[i])
					}
				}
			})

			t.Run("drain pending messages", func(t *testing.T) {
				drained := lane.DrainPending()
				if len(drained) != len(messages) {
					t.Errorf("DrainPending() returned %d messages, want %d", len(drained), len(messages))
				}

				for i, msg := range drained {
					if len(msg.Content) == 0 {
						t.Errorf("drained message %d has no content", i)
					}
					textContent, ok := msg.Content[0].(agent.TextContent)
					if !ok {
						t.Errorf("drained message %d content is not TextContent", i)
					}
					if textContent.Text != messages[i] {
						t.Errorf("drained message %d text = %q, want %q", i, textContent.Text, messages[i])
					}
				}
			})

			t.Run("verify queue cleared", func(t *testing.T) {
				status := lane.Status()
				if status.QueueDepth != 0 {
					t.Errorf("queue depth after drain = %d, want 0", status.QueueDepth)
				}

				pending := lane.Pending()
				if len(pending) != 0 {
					t.Errorf("Pending() after drain returned %d tasks, want 0", len(pending))
				}
			})
		})

		t.Run("submit and steer", func(t *testing.T) {
			messages := []string{"msg1", "msg2", "msg3"}
			for _, msg := range messages {
				lane.Submit(msg)
			}

			t.Run("steer and verify cancelled count", func(t *testing.T) {
				cancelled := lane.Steer()
				if cancelled != len(messages) {
					t.Errorf("Steer() returned %d, want %d", cancelled, len(messages))
				}
			})

			t.Run("verify queue cleared after steer", func(t *testing.T) {
				status := lane.Status()
				if status.QueueDepth != 0 {
					t.Errorf("queue depth after steer = %d, want 0", status.QueueDepth)
				}
			})
		})
	})
}
