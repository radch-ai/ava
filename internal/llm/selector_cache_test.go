package llm

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestSelectorCache_Get_CacheHit(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() {
		_ = client.Close()
	}()

	cache := NewSelectorCache(client)
	ctx := context.Background()

	recommendation := &ModelRecommendation{
		Provider:   "openai",
		Model:      "gpt-4",
		Confidence: 0.95,
	}

	err := cache.Set(ctx, "user123", "session456", "pro", recommendation)
	assert.NoError(t, err)

	result, err := cache.Get(ctx, "user123", "session456", "pro")
	assert.NoError(t, err)
	assert.Equal(t, recommendation.Provider, result.Provider)
	assert.Equal(t, recommendation.Model, result.Model)
	assert.Equal(t, recommendation.Confidence, result.Confidence)
}

func TestSelectorCache_Get_CacheMiss(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	cache := NewSelectorCache(client)
	ctx := context.Background()

	result, err := cache.Get(ctx, "user999", "session999", "pro")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Nil(t, result)
}

func TestSelectorCache_Set_TTL_Pro(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	cache := NewSelectorCache(client)
	ctx := context.Background()

	recommendation := &ModelRecommendation{
		Provider:   "openai",
		Model:      "gpt-4",
		Confidence: 0.95,
	}

	err := cache.Set(ctx, "user1", "session1", "pro", recommendation)
	assert.NoError(t, err)

	s.FastForward(14 * time.Minute)
	result, err := cache.Get(ctx, "user1", "session1", "pro")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	s.FastForward(2 * time.Minute)
	_, err = cache.Get(ctx, "user1", "session1", "pro")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSelectorCache_Set_TTL_Standard(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	cache := NewSelectorCache(client)
	ctx := context.Background()

	recommendation := &ModelRecommendation{
		Provider:   "openai",
		Model:      "gpt-4",
		Confidence: 0.95,
	}

	err := cache.Set(ctx, "user2", "session2", "standard", recommendation)
	assert.NoError(t, err)

	s.FastForward(29 * time.Minute)
	result, err := cache.Get(ctx, "user2", "session2", "standard")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	s.FastForward(2 * time.Minute)
	_, err = cache.Get(ctx, "user2", "session2", "standard")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSelectorCache_Set_TTL_Lite(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() {
		_ = client.Close()
	}()

	cache := NewSelectorCache(client)
	ctx := context.Background()

	recommendation := &ModelRecommendation{
		Provider:   "openai",
		Model:      "gpt-4",
		Confidence: 0.95,
	}

	err := cache.Set(ctx, "user3", "session3", "lite", recommendation)
	assert.NoError(t, err)

	s.FastForward(59 * time.Minute)
	result, err := cache.Get(ctx, "user3", "session3", "lite")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	s.FastForward(2 * time.Minute)
	_, err = cache.Get(ctx, "user3", "session3", "lite")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestSelectorCache_Set_Overwrite(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	cache := NewSelectorCache(client)
	ctx := context.Background()

	oldRecommendation := &ModelRecommendation{
		Provider:   "openai",
		Model:      "gpt-4",
		Confidence: 0.95,
	}

	newRecommendation := &ModelRecommendation{
		Provider:   "anthropic",
		Model:      "claude-3-sonnet",
		Confidence: 0.88,
	}

	err := cache.Set(ctx, "user4", "session4", "pro", oldRecommendation)
	assert.NoError(t, err)

	err = cache.Set(ctx, "user4", "session4", "pro", newRecommendation)
	assert.NoError(t, err)

	result, err := cache.Get(ctx, "user4", "session4", "pro")
	assert.NoError(t, err)
	assert.Equal(t, "anthropic", result.Provider)
	assert.Equal(t, "claude-3-sonnet", result.Model)
}

func TestSelectorCache_KeyFormat(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	cache := NewSelectorCache(client)

	tests := []struct {
		userID    string
		sessionID string
		expected  string
	}{
		{"user123", "session456", "nd_recommendation:user123:session456"},
		{"test-user", "test-session", "nd_recommendation:test-user:test-session"},
	}

	for _, tt := range tests {
		t.Run(tt.userID+"-"+tt.sessionID, func(t *testing.T) {
			key := cache.cacheKey(tt.userID, tt.sessionID)
			assert.Equal(t, tt.expected, key)
		})
	}
}

func TestNewSelectorCache(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	cache := NewSelectorCache(client)

	assert.NotNil(t, cache)
	assert.Equal(t, client, cache.client)
}

func TestSelectorCache_ContextTimeout(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	cache := NewSelectorCache(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	time.Sleep(20 * time.Millisecond)

	result, err := cache.Get(ctx, "user1", "session1", "pro")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestSelectorCache_GetTTL_Pro(t *testing.T) {
	cache := NewSelectorCache(nil)
	ttl := cache.getTTL("pro")
	assert.Equal(t, 15*time.Minute, ttl)
}

func TestSelectorCache_GetTTL_Standard(t *testing.T) {
	cache := NewSelectorCache(nil)
	ttl := cache.getTTL("standard")
	assert.Equal(t, 30*time.Minute, ttl)
}

func TestSelectorCache_GetTTL_Lite(t *testing.T) {
	cache := NewSelectorCache(nil)
	ttl := cache.getTTL("lite")
	assert.Equal(t, 60*time.Minute, ttl)
}

func TestSelectorCache_GetTTL_Default(t *testing.T) {
	cache := NewSelectorCache(nil)
	ttl := cache.getTTL("unknown")
	assert.Equal(t, 30*time.Minute, ttl)
}
