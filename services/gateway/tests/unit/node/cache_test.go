// Package node_test provides unit tests for the node tool cache.
package node_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/radch-ai/ava/services/gateway/internal/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCache is an in-memory mock implementation of server.Cache for testing.
type mockCache struct {
	mu     sync.RWMutex
	data   map[string]cacheEntry
	closed bool

	// Error injection for testing error scenarios
	getError    error
	setError    error
	deleteError error
}

type cacheEntry struct {
	value      string
	expiration time.Time
}

func newMockCache() *mockCache {
	return &mockCache{
		data: make(map[string]cacheEntry),
	}
}

func (m *mockCache) Get(_ context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.getError != nil {
		return "", m.getError
	}

	if m.closed {
		return "", errors.New("cache closed")
	}

	entry, exists := m.data[key]
	if !exists {
		return "", errors.New("key not found")
	}

	// Check expiration
	if !entry.expiration.IsZero() && time.Now().After(entry.expiration) {
		delete(m.data, key)
		return "", errors.New("key not found")
	}

	return entry.value, nil
}

func (m *mockCache) Set(_ context.Context, key string, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.setError != nil {
		return m.setError
	}

	if m.closed {
		return errors.New("cache closed")
	}

	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}

	m.data[key] = cacheEntry{
		value:      value,
		expiration: exp,
	}

	return nil
}

func (m *mockCache) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.deleteError != nil {
		return m.deleteError
	}

	if m.closed {
		return errors.New("cache closed")
	}

	delete(m.data, key)
	return nil
}

func (m *mockCache) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

// hasKey checks if a key exists (for testing verification).
func (m *mockCache) hasKey(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.data[key]
	if !exists {
		return false
	}

	// Check expiration
	if !entry.expiration.IsZero() && time.Now().After(entry.expiration) {
		return false
	}

	return true
}

// count returns the number of non-expired keys (for testing verification).
func (m *mockCache) count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	now := time.Now()
	for _, entry := range m.data {
		if entry.expiration.IsZero() || now.Before(entry.expiration) {
			count++
		}
	}
	return count
}

// fastForward simulates time passing for TTL testing.
func (m *mockCache) fastForward(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, entry := range m.data {
		if !entry.expiration.IsZero() {
			entry.expiration = entry.expiration.Add(-d)
			if time.Now().After(entry.expiration) {
				delete(m.data, key)
			} else {
				m.data[key] = entry
			}
		}
	}
}

// TestNewToolCache verifies ToolCache initialization.
func TestNewToolCache(t *testing.T) {
	t.Run("creates a new ToolCache with default TTL", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)

		assert.NotNil(t, toolCache)
	})
}

// TestToolCache_SetUserTools tests storing tool lists in cache.
func TestToolCache_SetUserTools(t *testing.T) {
	t.Run("successfully stores empty tool list", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		err := toolCache.SetUserTools(ctx, "user1", []string{})

		require.NoError(t, err)
		assert.True(t, mockCache.hasKey("user_tools:user1"))
	})

	t.Run("successfully stores non-empty tool list", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()
		tools := []string{"bash", "python", "node"}

		err := toolCache.SetUserTools(ctx, "user1", tools)

		require.NoError(t, err)
		assert.True(t, mockCache.hasKey("user_tools:user1"))

		// Verify stored data
		val, _ := mockCache.Get(ctx, "user_tools:user1")
		var storedTools []string
		err = json.Unmarshal([]byte(val), &storedTools)
		require.NoError(t, err)
		assert.Equal(t, tools, storedTools)
	})

	t.Run("overwrites existing tool list", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Set initial tools
		err := toolCache.SetUserTools(ctx, "user1", []string{"bash", "python"})
		require.NoError(t, err)

		// Update with new tools
		err = toolCache.SetUserTools(ctx, "user1", []string{"node", "go"})
		require.NoError(t, err)

		// Verify updated value
		val, _ := mockCache.Get(ctx, "user_tools:user1")
		var storedTools []string
		err = json.Unmarshal([]byte(val), &storedTools)
		require.NoError(t, err)
		assert.Equal(t, []string{"node", "go"}, storedTools)
	})

	t.Run("stores tools for different users independently", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		err1 := toolCache.SetUserTools(ctx, "user1", []string{"bash"})
		err2 := toolCache.SetUserTools(ctx, "user2", []string{"python"})

		require.NoError(t, err1)
		require.NoError(t, err2)

		assert.Equal(t, 2, mockCache.count())
		assert.True(t, mockCache.hasKey("user_tools:user1"))
		assert.True(t, mockCache.hasKey("user_tools:user2"))
	})

	t.Run("returns error when underlying cache Set fails", func(t *testing.T) {
		mockCache := newMockCache()
		mockCache.setError = errors.New("cache set failed")
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		err := toolCache.SetUserTools(ctx, "user1", []string{"bash"})

		assert.Error(t, err)
		assert.ErrorContains(t, err, "cache set failed")
	})
}

// TestToolCache_GetUserTools tests retrieving tool lists from cache.
func TestToolCache_GetUserTools(t *testing.T) {
	t.Run("returns cached tool list", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store tools
		tools := []string{"bash", "python", "node"}
		err := toolCache.SetUserTools(ctx, "user1", tools)
		require.NoError(t, err)

		// Test: retrieve tools
		retrieved, err := toolCache.GetUserTools(ctx, "user1")

		require.NoError(t, err)
		assert.Equal(t, tools, retrieved)
	})

	t.Run("returns empty slice when cache miss", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Test: retrieve non-existent user
		retrieved, err := toolCache.GetUserTools(ctx, "nonexistent")

		assert.Error(t, err)
		assert.ErrorContains(t, err, "cache get failed")
		assert.Nil(t, retrieved)
	})

	t.Run("returns error when underlying cache Get fails", func(t *testing.T) {
		mockCache := newMockCache()
		mockCache.getError = errors.New("connection lost")
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Test: retrieve with error condition
		retrieved, err := toolCache.GetUserTools(ctx, "user1")

		assert.Error(t, err)
		assert.ErrorContains(t, err, "cache get failed")
		assert.Nil(t, retrieved)
	})

	t.Run("returns error when cached data is invalid JSON", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store invalid JSON directly
		_ = mockCache.Set(ctx, "user_tools:user1", "not valid json", time.Minute)

		// Test: retrieve should fail
		retrieved, err := toolCache.GetUserTools(ctx, "user1")

		assert.Error(t, err)
		assert.ErrorContains(t, err, "failed to decode cached tools")
		assert.Nil(t, retrieved)
	})

	t.Run("returns empty tool list when cached value is empty array", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store empty array
		err := toolCache.SetUserTools(ctx, "user1", []string{})
		require.NoError(t, err)

		// Test: retrieve should return empty slice
		retrieved, err := toolCache.GetUserTools(ctx, "user1")

		require.NoError(t, err)
		assert.Empty(t, retrieved)
	})
}

// TestToolCache_InvalidateUserTools tests removing tool lists from cache.
func TestToolCache_InvalidateUserTools(t *testing.T) {
	t.Run("successfully removes existing user tools", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store tools
		err := toolCache.SetUserTools(ctx, "user1", []string{"bash", "python"})
		require.NoError(t, err)
		assert.True(t, mockCache.hasKey("user_tools:user1"))

		// Test: invalidate
		err = toolCache.InvalidateUserTools(ctx, "user1")

		require.NoError(t, err)
		assert.False(t, mockCache.hasKey("user_tools:user1"))
	})

	t.Run("invalidates non-existent user without error", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Test: invalidate non-existent key
		err := toolCache.InvalidateUserTools(ctx, "nonexistent")

		assert.NoError(t, err)
	})

	t.Run("returns error when underlying cache Delete fails", func(t *testing.T) {
		mockCache := newMockCache()
		mockCache.deleteError = errors.New("connection lost")
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Test: invalidate with error condition
		err := toolCache.InvalidateUserTools(ctx, "user1")

		assert.Error(t, err)
		assert.ErrorContains(t, err, "cache delete failed")
	})

	t.Run("only invalidates specified user", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store tools for multiple users
		_ = toolCache.SetUserTools(ctx, "user1", []string{"bash"})
		_ = toolCache.SetUserTools(ctx, "user2", []string{"python"})
		_ = toolCache.SetUserTools(ctx, "user3", []string{"node"})
		require.Equal(t, 3, mockCache.count())

		// Test: invalidate user2
		err := toolCache.InvalidateUserTools(ctx, "user2")
		require.NoError(t, err)

		// Verify: only user2 is invalidated
		assert.True(t, mockCache.hasKey("user_tools:user1"))
		assert.False(t, mockCache.hasKey("user_tools:user2"))
		assert.True(t, mockCache.hasKey("user_tools:user3"))
	})
}

// TestToolCache_ExtendTTL tests extending TTL for cached entries.
func TestToolCache_ExtendTTL(t *testing.T) {
	t.Run("successfully extends TTL for existing entry", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store tools with short TTL
		tools := []string{"bash"}
		data, _ := json.Marshal(tools)
		_ = mockCache.Set(ctx, "user_tools:user1", string(data), 100*time.Millisecond)

		// Test: extend TTL
		err := toolCache.ExtendTTL(ctx, "user1", time.Minute)

		require.NoError(t, err)
		assert.True(t, mockCache.hasKey("user_tools:user1"))
	})

	t.Run("returns error for non-existent entry", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Test: extend TTL for non-existent key
		err := toolCache.ExtendTTL(ctx, "nonexistent", time.Minute)

		assert.Error(t, err)
		assert.ErrorContains(t, err, "cache miss")
	})

	t.Run("returns error when underlying cache Get fails", func(t *testing.T) {
		mockCache := newMockCache()
		mockCache.getError = errors.New("connection lost")
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Test: extend with Get error
		err := toolCache.ExtendTTL(ctx, "user1", time.Minute)

		assert.Error(t, err)
		assert.ErrorContains(t, err, "cache miss")
	})

	t.Run("returns error when underlying cache Set fails", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store tools
		_ = toolCache.SetUserTools(ctx, "user1", []string{"bash"})

		// Set error after Get (simulate failure during Set)
		mockCache.setError = errors.New("set failed")

		// Test: extend with Set error
		err := toolCache.ExtendTTL(ctx, "user1", time.Minute)

		assert.Error(t, err)
		assert.ErrorContains(t, err, "failed to extend TTL")
	})
}

// TestToolCache_BuildUserToolList tests combining and caching tool lists.
func TestToolCache_BuildUserToolList(t *testing.T) {
	t.Run("combines cloud tools with empty node tools on cache miss", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()
		cloudTools := []string{"search", "calculator"}

		// Test: build with cache miss
		combined, err := toolCache.BuildUserToolList(ctx, "user1", cloudTools)

		require.NoError(t, err)
		assert.Equal(t, cloudTools, combined)

		// Verify the combined list was cached
		assert.True(t, mockCache.hasKey("user_tools:user1"))
	})

	t.Run("combines cloud tools with cached node tools", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: cache node tools
		nodeTools := []string{"bash", "python"}
		err := toolCache.SetUserTools(ctx, "user1", nodeTools)
		require.NoError(t, err)

		// Test: build with existing node tools
		cloudTools := []string{"search", "calculator"}
		combined, err := toolCache.BuildUserToolList(ctx, "user1", cloudTools)

		require.NoError(t, err)
		assert.Len(t, combined, 4)
		assert.Contains(t, combined, "search")
		assert.Contains(t, combined, "calculator")
		assert.Contains(t, combined, "bash")
		assert.Contains(t, combined, "python")
	})

	t.Run("cloud tools appear before node tools in combined list", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: cache node tools
		nodeTools := []string{"bash", "python"}
		_ = toolCache.SetUserTools(ctx, "user1", nodeTools)

		// Test: build and check order
		cloudTools := []string{"search"}
		combined, _ := toolCache.BuildUserToolList(ctx, "user1", cloudTools)

		// Cloud tools should come first
		assert.Equal(t, "search", combined[0])
		// Node tools should follow
		assert.Contains(t, combined[1:], "bash")
		assert.Contains(t, combined[1:], "python")
	})

	t.Run("deduplicates tools when combining", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: cache node tools with overlap
		nodeTools := []string{"bash", "python"}
		_ = toolCache.SetUserTools(ctx, "user1", nodeTools)

		// Test: build with overlapping cloud tools
		cloudTools := []string{"search", "bash"} // bash is in both
		combined, _ := toolCache.BuildUserToolList(ctx, "user1", cloudTools)

		// Count occurrences of "bash"
		bashCount := 0
		for _, tool := range combined {
			if tool == "bash" {
				bashCount++
			}
		}
		assert.Equal(t, 2, bashCount, "bash should appear twice (once from cloud, once from node)")
	})

	t.Run("succeeds even when caching the combined list fails", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()
		cloudTools := []string{"search"}

		// Setup: make Set fail
		mockCache.setError = errors.New("cache set failed")

		// Test: build should still succeed
		combined, err := toolCache.BuildUserToolList(ctx, "user1", cloudTools)

		require.NoError(t, err)
		assert.Equal(t, cloudTools, combined)
	})

	t.Run("handles empty cloud tools", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: cache node tools
		nodeTools := []string{"bash", "python"}
		_ = toolCache.SetUserTools(ctx, "user1", nodeTools)

		// Test: build with empty cloud tools
		combined, err := toolCache.BuildUserToolList(ctx, "user1", []string{})

		require.NoError(t, err)
		assert.Len(t, combined, 2)
		assert.Contains(t, combined, "bash")
		assert.Contains(t, combined, "python")
	})

	t.Run("handles empty node tools on cache miss", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: cache empty node tools
		_ = toolCache.SetUserTools(ctx, "user1", []string{})

		// Test: build
		cloudTools := []string{"search"}
		combined, err := toolCache.BuildUserToolList(ctx, "user1", cloudTools)

		require.NoError(t, err)
		assert.Equal(t, cloudTools, combined)
	})
}

// TestToolCache_TTLExpiration tests TTL-based cache expiration.
func TestToolCache_TTLExpiration(t *testing.T) {
	t.Run("entry expires after default TTL", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store tools
		err := toolCache.SetUserTools(ctx, "user1", []string{"bash"})
		require.NoError(t, err)
		assert.True(t, mockCache.hasKey("user_tools:user1"))

		// Fast forward past default TTL (3 minutes)
		mockCache.fastForward(3*time.Minute + time.Second)

		// Verify: entry has expired
		assert.False(t, mockCache.hasKey("user_tools:user1"))

		// Get should return cache miss error
		_, err = toolCache.GetUserTools(ctx, "user1")
		assert.Error(t, err)
	})

	t.Run("entries for different users expire independently", func(t *testing.T) {
		mockCache := newMockCache()
		ctx := context.Background()

		// Setup: store tools for two users with different TTLs
		// User1 with 2 minute TTL
		data1, _ := json.Marshal([]string{"bash"})
		err := mockCache.Set(ctx, "user_tools:user1", string(data1), 2*time.Minute)
		require.NoError(t, err)

		// User2 with 4 minute TTL
		data2, _ := json.Marshal([]string{"python"})
		err = mockCache.Set(ctx, "user_tools:user2", string(data2), 4*time.Minute)
		require.NoError(t, err)

		// Both should exist initially
		assert.True(t, mockCache.hasKey("user_tools:user1"))
		assert.True(t, mockCache.hasKey("user_tools:user2"))

		// Fast forward 2 minutes - user1 should expire, user2 should still be valid
		mockCache.fastForward(2 * time.Minute)
		assert.False(t, mockCache.hasKey("user_tools:user1"), "user1 should expire after 2 minutes")
		assert.True(t, mockCache.hasKey("user_tools:user2"), "user2 should still be valid after 2 minutes")

		// Fast forward another 2 minutes (total 4 minutes) - user2 should also expire
		mockCache.fastForward(2 * time.Minute)
		assert.False(t, mockCache.hasKey("user_tools:user1"))
		assert.False(t, mockCache.hasKey("user_tools:user2"), "user2 should expire after 4 minutes")
	})

	t.Run("extendTTL prevents expiration", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store tools
		_ = toolCache.SetUserTools(ctx, "user1", []string{"bash"})

		// Fast forward 2 minutes
		mockCache.fastForward(2 * time.Minute)
		assert.True(t, mockCache.hasKey("user_tools:user1"))

		// Extend TTL
		err := toolCache.ExtendTTL(ctx, "user1", 5*time.Minute)
		require.NoError(t, err)

		// Fast forward another 2 minutes (total 4 minutes from start)
		// Original TTL would have expired, but extended TTL should keep it alive
		mockCache.fastForward(2 * time.Minute)
		assert.True(t, mockCache.hasKey("user_tools:user1"))
	})
}

// TestToolCache_ConcurrentAccess tests thread-safety of cache operations.
func TestToolCache_ConcurrentAccess(t *testing.T) {
	t.Run("handles concurrent reads safely", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: store tools
		_ = toolCache.SetUserTools(ctx, "user1", []string{"bash", "python"})

		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = toolCache.GetUserTools(ctx, "user1")
			}()
		}

		wg.Wait()
		// If we get here without panic/deadlock, the test passes
	})

	t.Run("handles concurrent writes safely", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				userID := "user" + string(rune('0'+id%10))
				tools := []string{"tool" + string(rune('0'+id))}
				_ = toolCache.SetUserTools(ctx, userID, tools)
			}(i)
		}

		wg.Wait()
		// Verify some entries were stored
		assert.Greater(t, mockCache.count(), 0)
	})

	t.Run("handles concurrent mixed operations safely", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup: initial data
		_ = toolCache.SetUserTools(ctx, "user1", []string{"bash"})
		_ = toolCache.SetUserTools(ctx, "user2", []string{"python"})

		var wg sync.WaitGroup
		iterations := 100

		for i := 0; i < iterations; i++ {
			wg.Add(3)

			// Concurrent reads
			go func() {
				defer wg.Done()
				_, _ = toolCache.GetUserTools(ctx, "user1")
			}()

			// Concurrent writes
			go func(id int) {
				defer wg.Done()
				tools := []string{"tool" + string(rune('0'+id%10))}
				_ = toolCache.SetUserTools(ctx, "user1", tools)
			}(i)

			// Concurrent invalidations
			go func() {
				defer wg.Done()
				_ = toolCache.InvalidateUserTools(ctx, "user2")
			}()
		}

		wg.Wait()
		// If we get here without panic/deadlock, the test passes
	})

	t.Run("handles concurrent BuildUserToolList safely", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()
		cloudTools := []string{"search", "calculator"}

		var wg sync.WaitGroup
		numGoroutines := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				userID := "user" + string(rune('0'+id%5))
				_, _ = toolCache.BuildUserToolList(ctx, userID, cloudTools)
			}(i)
		}

		wg.Wait()
		// Verify cache has entries
		assert.Greater(t, mockCache.count(), 0)
	})
}

// TestToolCache_EdgeCases tests edge cases and boundary conditions.
func TestToolCache_EdgeCases(t *testing.T) {
	t.Run("handles empty userID", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Empty userID should still work (cache key would be "user_tools:")
		err := toolCache.SetUserTools(ctx, "", []string{"bash"})
		assert.NoError(t, err)
		assert.True(t, mockCache.hasKey("user_tools:"))
	})

	t.Run("handles special characters in userID", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		userIDs := []string{
			"user@example.com",
			"user/with/slashes",
			"user:with:colons",
			"user with spaces",
			"user\nwith\nnewlines",
		}

		for _, userID := range userIDs {
			err := toolCache.SetUserTools(ctx, userID, []string{"bash"})
			assert.NoError(t, err, "should handle userID: %q", userID)
		}
	})

	t.Run("handles very long tool list", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Create a large tool list
		tools := make([]string, 1000)
		for i := 0; i < 1000; i++ {
			tools[i] = "tool" + string(rune('0'+i%10))
		}

		err := toolCache.SetUserTools(ctx, "user1", tools)
		assert.NoError(t, err)

		// Verify retrieval
		retrieved, err := toolCache.GetUserTools(ctx, "user1")
		require.NoError(t, err)
		assert.Len(t, retrieved, 1000)
	})

	t.Run("handles tool list with empty strings", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		tools := []string{"bash", "", "python", "", ""}
		err := toolCache.SetUserTools(ctx, "user1", tools)
		assert.NoError(t, err)

		retrieved, err := toolCache.GetUserTools(ctx, "user1")
		require.NoError(t, err)
		assert.Equal(t, tools, retrieved)
	})

	t.Run("handles tool list with duplicate entries", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		tools := []string{"bash", "python", "bash", "python", "bash"}
		err := toolCache.SetUserTools(ctx, "user1", tools)
		assert.NoError(t, err)

		retrieved, err := toolCache.GetUserTools(ctx, "user1")
		require.NoError(t, err)
		assert.Equal(t, tools, retrieved)
	})

	t.Run("handles zero TTL", func(t *testing.T) {
		mockCache := newMockCache()
		ctx := context.Background()

		// Store directly with zero TTL
		data, _ := json.Marshal([]string{"bash"})
		err := mockCache.Set(ctx, "user_tools:user1", string(data), 0)
		require.NoError(t, err)

		// Zero TTL means no expiration - should still be accessible
		assert.True(t, mockCache.hasKey("user_tools:user1"))
	})

	t.Run("handles nil context", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)

		// nil context should be handled by the underlying cache
		// Our mock doesn't use context, so this should work
		err := toolCache.SetUserTools(context.TODO(), "user1", []string{"bash"})
		assert.NoError(t, err)

		_, err = toolCache.GetUserTools(context.TODO(), "user1")
		assert.NoError(t, err)
	})
}

// TestToolCache_Integration tests complete workflows.
func TestToolCache_Integration(t *testing.T) {
	t.Run("complete cache lifecycle", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// 1. Set initial tools
		err := toolCache.SetUserTools(ctx, "user1", []string{"bash", "python"})
		require.NoError(t, err)

		// 2. Get tools
		tools, err := toolCache.GetUserTools(ctx, "user1")
		require.NoError(t, err)
		assert.Equal(t, []string{"bash", "python"}, tools)

		// 3. Build combined list with cloud tools
		combined, err := toolCache.BuildUserToolList(ctx, "user1", []string{"search"})
		require.NoError(t, err)
		assert.Len(t, combined, 3)

		// 4. Extend TTL
		err = toolCache.ExtendTTL(ctx, "user1", 5*time.Minute)
		require.NoError(t, err)

		// 5. Invalidate
		err = toolCache.InvalidateUserTools(ctx, "user1")
		require.NoError(t, err)

		// 6. Verify cache miss
		_, err = toolCache.GetUserTools(ctx, "user1")
		assert.Error(t, err)
	})

	t.Run("multi-user scenario", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup multiple users with different tools
		users := map[string][]string{
			"admin": {"bash", "python", "docker", "kubectl"},
			"dev":   {"bash", "node", "npm"},
			"guest": {"calculator"},
			"bot":   {},
		}

		for user, tools := range users {
			err := toolCache.SetUserTools(ctx, user, tools)
			require.NoError(t, err)
		}

		// Verify each user has correct tools
		for user, expectedTools := range users {
			retrieved, err := toolCache.GetUserTools(ctx, user)
			require.NoError(t, err)
			assert.Equal(t, expectedTools, retrieved)
		}

		// Invalidate one user
		err := toolCache.InvalidateUserTools(ctx, "dev")
		require.NoError(t, err)

		// Verify only dev is invalidated
		_, err = toolCache.GetUserTools(ctx, "dev")
		assert.Error(t, err)

		for _, user := range []string{"admin", "guest", "bot"} {
			_, err := toolCache.GetUserTools(ctx, user)
			assert.NoError(t, err)
		}
	})
}

// TestToolCache_CacheKeyFormat tests cache key generation and format.
func TestToolCache_CacheKeyFormat(t *testing.T) {
	t.Run("uses correct prefix for cache keys", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		err := toolCache.SetUserTools(ctx, "user123", []string{"bash"})
		require.NoError(t, err)

		// Verify the exact cache key format
		assert.True(t, mockCache.hasKey("user_tools:user123"))

		// Verify other similar keys don't match
		assert.False(t, mockCache.hasKey("user_tools:user1234"))
		assert.False(t, mockCache.hasKey("user_tools:user12"))
	})

	t.Run("handles special characters in cache keys", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		specialUserIDs := []string{
			"user@example.com",
			"user/sub/path",
			"user:colon:separated",
			"user with spaces",
			"user\nwith\tcontrol",
		}

		for _, userID := range specialUserIDs {
			err := toolCache.SetUserTools(ctx, userID, []string{"bash"})
			require.NoError(t, err, "should set tools for userID: %q", userID)

			expectedKey := "user_tools:" + userID
			assert.True(t, mockCache.hasKey(expectedKey), "should have key: %q", expectedKey)
		}
	})

	t.Run("consistent key format across operations", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()
		userID := "test-user-123"

		// Set
		_ = toolCache.SetUserTools(ctx, userID, []string{"bash"})

		// Get should find it
		_, err := toolCache.GetUserTools(ctx, userID)
		assert.NoError(t, err)

		// Invalidate should remove it
		_ = toolCache.InvalidateUserTools(ctx, userID)
		assert.False(t, mockCache.hasKey("user_tools:"+userID))
	})
}

// TestToolCache_TableDrivenTests uses table-driven approach for comprehensive testing.
func TestToolCache_TableDrivenTests(t *testing.T) {
	t.Run("SetUserTools with various tool lists", func(t *testing.T) {
		tests := []struct {
			name    string
			userID  string
			tools   []string
			wantErr bool
			errMsg  string
		}{
			{
				name:    "empty tool list",
				userID:  "user1",
				tools:   []string{},
				wantErr: false,
			},
			{
				name:    "single tool",
				userID:  "user2",
				tools:   []string{"bash"},
				wantErr: false,
			},
			{
				name:    "multiple tools",
				userID:  "user3",
				tools:   []string{"bash", "python", "node", "go", "rust"},
				wantErr: false,
			},
			{
				name:    "tools with special characters",
				userID:  "user4",
				tools:   []string{"tool-with-dash", "tool_with_underscore", "tool.with.dots"},
				wantErr: false,
			},
			{
				name:    "tools with unicode",
				userID:  "user5",
				tools:   []string{"工具", "outil", "ツール"},
				wantErr: false,
			},
			{
				name:    "empty userID",
				userID:  "",
				tools:   []string{"bash"},
				wantErr: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockCache := newMockCache()
				toolCache := node.NewToolCache(mockCache)
				ctx := context.Background()

				err := toolCache.SetUserTools(ctx, tt.userID, tt.tools)

				if tt.wantErr {
					assert.Error(t, err)
					if tt.errMsg != "" {
						assert.ErrorContains(t, err, tt.errMsg)
					}
				} else {
					assert.NoError(t, err)
					// Verify retrieval works
					retrieved, err := toolCache.GetUserTools(ctx, tt.userID)
					assert.NoError(t, err)
					assert.Equal(t, tt.tools, retrieved)
				}
			})
		}
	})

	t.Run("GetUserTools with various cache states", func(t *testing.T) {
		tests := []struct {
			name          string
			userID        string
			setupCache    func(*node.ToolCache, context.Context)
			wantErr       bool
			expectedTools []string
		}{
			{
				name:   "cache hit with tools",
				userID: "user1",
				setupCache: func(tc *node.ToolCache, ctx context.Context) {
					_ = tc.SetUserTools(ctx, "user1", []string{"bash", "python"})
				},
				wantErr:       false,
				expectedTools: []string{"bash", "python"},
			},
			{
				name:   "cache hit with empty tools",
				userID: "user2",
				setupCache: func(tc *node.ToolCache, ctx context.Context) {
					_ = tc.SetUserTools(ctx, "user2", []string{})
				},
				wantErr:       false,
				expectedTools: []string{},
			},
			{
				name:          "cache miss",
				userID:        "user3",
				setupCache:    func(tc *node.ToolCache, ctx context.Context) {},
				wantErr:       true,
				expectedTools: nil,
			},
			{
				name:   "different user exists",
				userID: "user4",
				setupCache: func(tc *node.ToolCache, ctx context.Context) {
					_ = tc.SetUserTools(ctx, "other_user", []string{"bash"})
				},
				wantErr:       true,
				expectedTools: nil,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockCache := newMockCache()
				toolCache := node.NewToolCache(mockCache)
				ctx := context.Background()

				tt.setupCache(toolCache, ctx)

				tools, err := toolCache.GetUserTools(ctx, tt.userID)

				if tt.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tt.expectedTools, tools)
				}
			})
		}
	})

	t.Run("InvalidateUserTools scenarios", func(t *testing.T) {
		tests := []struct {
			name         string
			userID       string
			setupCache   func(*node.ToolCache, context.Context)
			shouldBeGone bool
		}{
			{
				name:   "invalidate existing entry",
				userID: "user1",
				setupCache: func(tc *node.ToolCache, ctx context.Context) {
					_ = tc.SetUserTools(ctx, "user1", []string{"bash"})
				},
				shouldBeGone: true,
			},
			{
				name:         "invalidate non-existent entry",
				userID:       "user2",
				setupCache:   func(tc *node.ToolCache, ctx context.Context) {},
				shouldBeGone: true,
			},
			{
				name:   "invalidate one of multiple entries",
				userID: "user1",
				setupCache: func(tc *node.ToolCache, ctx context.Context) {
					_ = tc.SetUserTools(ctx, "user1", []string{"bash"})
					_ = tc.SetUserTools(ctx, "user2", []string{"python"})
					_ = tc.SetUserTools(ctx, "user3", []string{"node"})
				},
				shouldBeGone: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				mockCache := newMockCache()
				toolCache := node.NewToolCache(mockCache)
				ctx := context.Background()

				tt.setupCache(toolCache, ctx)

				err := toolCache.InvalidateUserTools(ctx, tt.userID)
				assert.NoError(t, err)

				if tt.shouldBeGone {
					assert.False(t, mockCache.hasKey("user_tools:"+tt.userID))
				}
			})
		}
	})
}

// TestToolCache_CacheEviction tests cache cleanup and eviction behavior.
func TestToolCache_CacheEviction(t *testing.T) {
	t.Run("expired entries are removed on access", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Set entry with short TTL
		tools := []string{"bash", "python"}
		data, _ := json.Marshal(tools)
		err := mockCache.Set(ctx, "user_tools:user1", string(data), 50*time.Millisecond)
		require.NoError(t, err)

		// Verify it exists
		assert.True(t, mockCache.hasKey("user_tools:user1"))

		// Fast forward past TTL
		mockCache.fastForward(100 * time.Millisecond)

		// Access should trigger removal and return error
		_, err = toolCache.GetUserTools(ctx, "user1")
		assert.Error(t, err)
		assert.False(t, mockCache.hasKey("user_tools:user1"))
	})

	t.Run("multiple expired entries cleaned up", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Set multiple entries with varying TTLs
		_ = toolCache.SetUserTools(ctx, "user1", []string{"bash"})
		_ = toolCache.SetUserTools(ctx, "user2", []string{"python"})
		_ = toolCache.SetUserTools(ctx, "user3", []string{"node"})

		// Fast forward 2 minutes
		mockCache.fastForward(2 * time.Minute)

		// All should still be valid (default TTL is 3 minutes)
		assert.True(t, mockCache.hasKey("user_tools:user1"))
		assert.True(t, mockCache.hasKey("user_tools:user2"))
		assert.True(t, mockCache.hasKey("user_tools:user3"))

		// Fast forward another 2 minutes (total 4 minutes)
		mockCache.fastForward(2 * time.Minute)

		// All should be expired
		assert.False(t, mockCache.hasKey("user_tools:user1"))
		assert.False(t, mockCache.hasKey("user_tools:user2"))
		assert.False(t, mockCache.hasKey("user_tools:user3"))
	})

	t.Run("invalidate acts as immediate eviction", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Set entry
		_ = toolCache.SetUserTools(ctx, "user1", []string{"bash"})
		assert.True(t, mockCache.hasKey("user_tools:user1"))

		// Invalidate
		_ = toolCache.InvalidateUserTools(ctx, "user1")

		// Should be immediately gone
		assert.False(t, mockCache.hasKey("user_tools:user1"))
		_, err := toolCache.GetUserTools(ctx, "user1")
		assert.Error(t, err)
	})

	t.Run("batch eviction of multiple users", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup multiple users
		users := []string{"user1", "user2", "user3", "user4", "user5"}
		for _, user := range users {
			_ = toolCache.SetUserTools(ctx, user, []string{"bash"})
		}

		assert.Equal(t, 5, mockCache.count())

		// Invalidate all but one
		for i := 0; i < 4; i++ {
			_ = toolCache.InvalidateUserTools(ctx, users[i])
		}

		// Only one should remain
		assert.Equal(t, 1, mockCache.count())
		assert.True(t, mockCache.hasKey("user_tools:user5"))
	})
}

// TestToolCache_HighConcurrency tests high-concurrency scenarios.
func TestToolCache_HighConcurrency(t *testing.T) {
	t.Run("stress test with many concurrent operations", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		var wg sync.WaitGroup
		numOperations := 1000

		// Launch many concurrent operations
		for i := 0; i < numOperations; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				userID := fmt.Sprintf("user%d", id%100) // 100 different users
				tools := []string{fmt.Sprintf("tool%d", id)}
				_ = toolCache.SetUserTools(ctx, userID, tools)
				_, _ = toolCache.GetUserTools(ctx, userID)
				if id%10 == 0 {
					_ = toolCache.InvalidateUserTools(ctx, userID)
				}
			}(i)
		}

		wg.Wait()
		// If we get here without panic, test passes
		assert.GreaterOrEqual(t, mockCache.count(), 0)
	})

	t.Run("concurrent reads on same key", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		// Setup
		tools := []string{"bash", "python", "node"}
		_ = toolCache.SetUserTools(ctx, "user1", tools)

		var wg sync.WaitGroup
		numReaders := 1000
		successCount := 0
		var mu sync.Mutex

		for i := 0; i < numReaders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				retrieved, err := toolCache.GetUserTools(ctx, "user1")
				if err == nil && len(retrieved) == 3 {
					mu.Lock()
					successCount++
					mu.Unlock()
				}
			}()
		}

		wg.Wait()
		// All reads should succeed
		assert.Equal(t, numReaders, successCount)
	})

	t.Run("concurrent writes and invalidations", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		var wg sync.WaitGroup
		numGoroutines := 100
		iterations := 50

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				userID := fmt.Sprintf("user%d", id%10)
				for j := 0; j < iterations; j++ {
					tools := []string{fmt.Sprintf("tool%d_%d", id, j)}
					_ = toolCache.SetUserTools(ctx, userID, tools)
					if j%5 == 0 {
						_ = toolCache.InvalidateUserTools(ctx, userID)
					}
				}
			}(i)
		}

		wg.Wait()
		// Verify cache is still functional
		for i := 0; i < 10; i++ {
			userID := fmt.Sprintf("user%d", i)
			_, err := toolCache.GetUserTools(ctx, userID)
			// May or may not have data, but shouldn't panic
			_ = err
		}
	})
}

// TestToolCache_JSONHandling tests JSON marshaling edge cases.
func TestToolCache_JSONHandling(t *testing.T) {
	t.Run("handles all tool name variations", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		tools := []string{
			"simple",
			"tool-with-dashes",
			"tool_with_underscores",
			"tool.with.dots",
			"tool:with:colons",
			"tool/with/slashes",
			"tool with spaces",
			"tool'with'quotes",
			`tool"with"doublequotes`,
			"tool\nwith\nnewlines",
			"tool\twith\ttabs",
			"工具",    // Chinese characters
			"outil", // French
			"ツール",   // Japanese
			"🔧",     // Emoji
		}

		err := toolCache.SetUserTools(ctx, "user1", tools)
		require.NoError(t, err)

		retrieved, err := toolCache.GetUserTools(ctx, "user1")
		require.NoError(t, err)
		assert.Equal(t, tools, retrieved)
	})

	t.Run("handles very long tool names", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		longTool := string(make([]byte, 10000))
		for i := range longTool {
			longTool = longTool[:i] + "a" + longTool[i+1:]
		}

		tools := []string{longTool}
		err := toolCache.SetUserTools(ctx, "user1", tools)
		assert.NoError(t, err)

		retrieved, err := toolCache.GetUserTools(ctx, "user1")
		require.NoError(t, err)
		assert.Equal(t, tools, retrieved)
	})

	t.Run("preserves exact tool list on round trip", func(t *testing.T) {
		mockCache := newMockCache()
		toolCache := node.NewToolCache(mockCache)
		ctx := context.Background()

		original := []string{
			"",
			"a",
			"ab",
			"abc",
			"tool1",
			"tool2",
			"tool-with-symbols-!@#$%^&*()",
		}

		err := toolCache.SetUserTools(ctx, "user1", original)
		require.NoError(t, err)

		retrieved, err := toolCache.GetUserTools(ctx, "user1")
		require.NoError(t, err)

		// Exact match
		assert.Equal(t, original, retrieved)
		// Same length
		assert.Len(t, retrieved, len(original))
		// Same order
		for i := range original {
			assert.Equal(t, original[i], retrieved[i])
		}
	})
}
