package agent_test

import (
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

func TestLane_NewLane(t *testing.T) {
	t.Run("creates lane with initialized fields", func(t *testing.T) {
		lane := agent.NewLane()

		if lane == nil {
			t.Fatal("NewLane() returned nil")
		}

		if lane.SteerCh() == nil {
			t.Error("SteerCh() returned nil channel")
		}
	})

	t.Run("steer channel is buffered with capacity 1", func(t *testing.T) {
		lane := agent.NewLane()

		lane.SignalSteer()

		ch := lane.SteerCh()
		select {
		case <-ch:
		default:
			t.Error("steer channel should have received signal")
		}
	})
}

func TestLane_Submit(t *testing.T) {
	t.Run("queues task and returns ID", func(t *testing.T) {
		lane := agent.NewLane()
		message := "test message"

		id := lane.Submit(message)

		if id == "" {
			t.Error("Submit() returned empty ID")
		}

		pending := lane.Pending()
		if len(pending) != 1 {
			t.Errorf("Pending() returned %d tasks, want 1", len(pending))
		}

		if pending[0].Message != message {
			t.Errorf("task message = %q, want %q", pending[0].Message, message)
		}

		if pending[0].ID != id {
			t.Errorf("task ID = %q, want %q", pending[0].ID, id)
		}
	})
}

func TestLane_Steer(t *testing.T) {
	t.Run("cancels pending tasks and returns count", func(t *testing.T) {
		lane := agent.NewLane()

		for i := 0; i < 3; i++ {
			lane.Submit("test message")
		}

		cancelled := lane.Steer()

		if cancelled != 3 {
			t.Errorf("expected Steer to return 3, got %d", cancelled)
		}

		pending := lane.Pending()
		if len(pending) != 0 {
			t.Errorf("expected pending to be empty after Steer, got %d tasks", len(pending))
		}
	})
}
