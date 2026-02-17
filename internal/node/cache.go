package node

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nevaria-ai/gateway/internal/server"
)

const (
	// defaultCacheTTL is the default time-to-live for cached tool lists.
	defaultCacheTTL = 3 * time.Minute

	// cacheKeyPrefix is the prefix for user tool cache keys.
	cacheKeyPrefix = "user_tools:"
)

// ToolCache manages caching of user tool lists.
type ToolCache struct {
	cache      server.Cache
	defaultTTL time.Duration
}

// NewToolCache creates a new ToolCache with the given underlying cache implementation.
// The default TTL is set to 3 minutes.
func NewToolCache(cache server.Cache) *ToolCache {
	return &ToolCache{
		cache:      cache,
		defaultTTL: defaultCacheTTL,
	}
}

// GetUserTools retrieves the cached tool list for a user.
// Returns an empty slice and an error if the cache miss occurs.
func (tc *ToolCache) GetUserTools(ctx context.Context, userID string) ([]string, error) {
	key := tc.cacheKey(userID)

	value, err := tc.cache.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("cache get failed: %w", err)
	}

	var tools []string
	if err := json.Unmarshal([]byte(value), &tools); err != nil {
		return nil, fmt.Errorf("failed to decode cached tools: %w", err)
	}

	return tools, nil
}

// SetUserTools caches the tool list for a user with the default TTL.
func (tc *ToolCache) SetUserTools(ctx context.Context, userID string, tools []string) error {
	key := tc.cacheKey(userID)

	data, err := json.Marshal(tools)
	if err != nil {
		return fmt.Errorf("failed to encode tools: %w", err)
	}

	if err := tc.cache.Set(ctx, key, string(data), tc.defaultTTL); err != nil {
		return fmt.Errorf("cache set failed: %w", err)
	}

	return nil
}

// InvalidateUserTools removes the cached tool list for a user.
func (tc *ToolCache) InvalidateUserTools(ctx context.Context, userID string) error {
	key := tc.cacheKey(userID)

	if err := tc.cache.Delete(ctx, key); err != nil {
		return fmt.Errorf("cache delete failed: %w", err)
	}

	return nil
}

// ExtendTTL extends the TTL for the cached tools of a user.
// Returns an error if the cache entry does not exist.
func (tc *ToolCache) ExtendTTL(ctx context.Context, userID string, ttl time.Duration) error {
	key := tc.cacheKey(userID)

	// Check if the key exists
	value, err := tc.cache.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("cache miss: %w", err)
	}

	// Set with new TTL
	if err := tc.cache.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("failed to extend TTL: %w", err)
	}

	return nil
}

// BuildUserToolList combines cached node tools with cloud tools.
// It retrieves the user's node tools from cache, combines them with cloud tools,
// and caches the combined list for future use.
func (tc *ToolCache) BuildUserToolList(ctx context.Context, userID string, cloudTools []string) ([]string, error) {
	// Get cached node tools
	nodeTools, err := tc.GetUserTools(ctx, userID)
	if err != nil {
		// Cache miss means no node tools yet
		// Start with empty node tools list
		nodeTools = []string{}
	}

	// Combine cloud and node tools
	combined := append([]string{}, cloudTools...)
	combined = append(combined, nodeTools...)

	// Cache the combined list
	_ = tc.SetUserTools(ctx, userID, combined)

	return combined, nil
}

// cacheKey generates the cache key for a user's tool list.
func (tc *ToolCache) cacheKey(userID string) string {
	return cacheKeyPrefix + userID
}
