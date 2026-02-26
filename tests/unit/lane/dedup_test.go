package lane_test

import (
	"testing"

	"github.com/nevaria-ai/gateway/internal/lane"
)

func TestDedupCache_New(t *testing.T) {
	t.Run("creates cache with local field initialized", func(t *testing.T) {
		dedupCache := lane.NewDedupCache(nil)

		if dedupCache == nil {
			t.Fatal("NewDedupCache() returned nil")
		}

		if dedupCache.Local() == nil {
			t.Error("Local() returned nil cache")
		}
	})
}

func TestDedupCache_CheckAndProcess_FirstTime(t *testing.T) {
	dedupCache := lane.NewDedupCache(nil)

	shouldProcess, _ := dedupCache.Check("user1", "agent1", "msg1")

	if !shouldProcess {
		t.Error("expected shouldProcess to be true for first time")
	}
}

func TestDedupCache_CheckAndProcess_SecondTime(t *testing.T) {
	dedupCache := lane.NewDedupCache(nil)

	dedupCache.Mark("user1", "agent1", "msg1")
	shouldProcess, _ := dedupCache.Check("user1", "agent1", "msg1")

	if shouldProcess {
		t.Error("expected shouldProcess to be false after marking")
	}
}

func TestDedupCache_CheckAndProcess_DifferentUsers(t *testing.T) {
	dedupCache := lane.NewDedupCache(nil)

	dedupCache.Mark("user1", "agent1", "msg1")
	shouldProcess1, _ := dedupCache.Check("user1", "agent1", "msg1")
	shouldProcess2, _ := dedupCache.Check("user2", "agent1", "msg1")

	if shouldProcess1 {
		t.Error("expected shouldProcess to be false for user1 after marking")
	}
	if !shouldProcess2 {
		t.Error("expected shouldProcess to be true for user2")
	}
}
