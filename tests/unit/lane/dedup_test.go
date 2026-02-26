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
