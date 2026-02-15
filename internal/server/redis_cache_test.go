package server

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mockRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()

	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})

	return s, client
}

func TestRedisCache_Get(t *testing.T) {
	t.Run("returns value when key exists", func(t *testing.T) {
		s, client := mockRedis(t)
		defer s.Close()

		cache := NewRedisCache(client)
		ctx := context.Background()

		// Setup: store a value
		err := cache.Set(ctx, "test-key", "test-value", time.Minute)
		require.NoError(t, err)

		// Test: retrieve the value
		val, err := cache.Get(ctx, "test-key")
		assert.NoError(t, err)
		assert.Equal(t, "test-value", val)
	})

	t.Run("returns error when key does not exist", func(t *testing.T) {
		s, client := mockRedis(t)
		defer s.Close()

		cache := NewRedisCache(client)
		ctx := context.Background()

		// Test: retrieve non-existent key
		val, err := cache.Get(ctx, "non-existent-key")
		assert.Error(t, err)
		assert.ErrorContains(t, err, "key not found")
		assert.Empty(t, val)
	})

	t.Run("returns error on Redis failure", func(t *testing.T) {
		s, client := mockRedis(t)
		cache := NewRedisCache(client)
		ctx := context.Background()

		// Close the miniredis to simulate connection failure
		s.Close()

		// Test: retrieve value with closed connection
		_, err := cache.Get(ctx, "test-key")
		assert.Error(t, err)
	})
}

func TestRedisCache_Set(t *testing.T) {
	t.Run("sets value successfully", func(t *testing.T) {
		s, client := mockRedis(t)
		defer s.Close()

		cache := NewRedisCache(client)
		ctx := context.Background()

		// Test: set a value
		err := cache.Set(ctx, "test-key", "test-value", time.Minute)
		assert.NoError(t, err)

		// Verify: value was set
		val, err := cache.Get(ctx, "test-key")
		assert.NoError(t, err)
		assert.Equal(t, "test-value", val)
	})

	t.Run("sets value with TTL and expires", func(t *testing.T) {
		s, client := mockRedis(t)
		defer s.Close()

		cache := NewRedisCache(client)
		ctx := context.Background()

		// Test: set a value with short TTL
		err := cache.Set(ctx, "test-key", "test-value", time.Second)
		require.NoError(t, err)

		// Verify: value exists immediately
		val, err := cache.Get(ctx, "test-key")
		assert.NoError(t, err)
		assert.Equal(t, "test-value", val)

		// Fast-forward time past TTL
		s.FastForward(time.Second + time.Millisecond)

		// Verify: value has expired
		_, err = cache.Get(ctx, "test-key")
		assert.Error(t, err)
		assert.ErrorContains(t, err, "key not found")
	})

	t.Run("returns error on Redis failure", func(t *testing.T) {
		s, client := mockRedis(t)
		cache := NewRedisCache(client)
		ctx := context.Background()

		// Close the miniredis to simulate connection failure
		s.Close()

		// Test: set value with closed connection
		err := cache.Set(ctx, "test-key", "test-value", time.Minute)
		assert.Error(t, err)
	})

	t.Run("updates existing key", func(t *testing.T) {
		s, client := mockRedis(t)
		defer s.Close()

		cache := NewRedisCache(client)
		ctx := context.Background()

		// Setup: set initial value
		err := cache.Set(ctx, "test-key", "initial-value", time.Minute)
		require.NoError(t, err)

		// Test: update the value
		err = cache.Set(ctx, "test-key", "updated-value", time.Minute)
		assert.NoError(t, err)

		// Verify: value was updated
		val, err := cache.Get(ctx, "test-key")
		assert.NoError(t, err)
		assert.Equal(t, "updated-value", val)
	})
}

func TestRedisCache_Delete(t *testing.T) {
	t.Run("deletes existing key", func(t *testing.T) {
		s, client := mockRedis(t)
		defer s.Close()

		cache := NewRedisCache(client)
		ctx := context.Background()

		// Setup: store a value
		err := cache.Set(ctx, "test-key", "test-value", time.Minute)
		require.NoError(t, err)

		// Verify: key exists
		val, err := cache.Get(ctx, "test-key")
		assert.NoError(t, err)
		assert.Equal(t, "test-value", val)

		// Test: delete the key
		err = cache.Delete(ctx, "test-key")
		assert.NoError(t, err)

		// Verify: key no longer exists
		_, err = cache.Get(ctx, "test-key")
		assert.Error(t, err)
		assert.ErrorContains(t, err, "key not found")
	})

	t.Run("deletes non-existent key without error", func(t *testing.T) {
		s, client := mockRedis(t)
		defer s.Close()

		cache := NewRedisCache(client)
		ctx := context.Background()

		// Test: delete non-existent key
		err := cache.Delete(ctx, "non-existent-key")
		assert.NoError(t, err)
	})

	t.Run("returns error on Redis failure", func(t *testing.T) {
		s, client := mockRedis(t)
		cache := NewRedisCache(client)
		ctx := context.Background()

		// Close the miniredis to simulate connection failure
		s.Close()

		// Test: delete with closed connection
		err := cache.Delete(ctx, "test-key")
		assert.Error(t, err)
	})
}

func TestRedisCache_Integration(t *testing.T) {
	t.Run("complete cache lifecycle", func(t *testing.T) {
		s, client := mockRedis(t)
		defer s.Close()

		cache := NewRedisCache(client)
		ctx := context.Background()

		// 1. Set initial value
		err := cache.Set(ctx, "user:123", "session-data", 5*time.Minute)
		require.NoError(t, err)

		// 2. Get value
		val, err := cache.Get(ctx, "user:123")
		require.NoError(t, err)
		assert.Equal(t, "session-data", val)

		// 3. Update value
		err = cache.Set(ctx, "user:123", "updated-session-data", 5*time.Minute)
		require.NoError(t, err)

		// 4. Verify update
		val, err = cache.Get(ctx, "user:123")
		require.NoError(t, err)
		assert.Equal(t, "updated-session-data", val)

		// 5. Delete value
		err = cache.Delete(ctx, "user:123")
		require.NoError(t, err)

		// 6. Verify deletion
		_, err = cache.Get(ctx, "user:123")
		assert.Error(t, err)
	})
}
