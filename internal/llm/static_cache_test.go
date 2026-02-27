package llm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewStaticCache(t *testing.T) {
	cache := NewStaticCache()

	assert.NotNil(t, cache)
	assert.NotNil(t, cache.store)
	assert.Equal(t, time.Duration(0), cache.refreshInterval)
}

func TestStaticCache_Get_AutoRefresh(t *testing.T) {
	cache := NewStaticCache()
	cache.SetRefreshInterval(10 * time.Millisecond)

	var fetchCount int32
	cache.fetchFunc = func(ctx context.Context, contextType string) (interface{}, error) {
		atomic.AddInt32(&fetchCount, 1)
		return "model-" + contextType, nil
	}

	ctx := context.Background()

	result, err := cache.Get(ctx, "cron")
	assert.NoError(t, err)
	assert.Equal(t, "model-cron", result)
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetchCount))

	result, err = cache.Get(ctx, "cron")
	assert.NoError(t, err)
	assert.Equal(t, "model-cron", result)
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetchCount), "should not fetch again on cache hit")

	time.Sleep(15 * time.Millisecond)

	result, err = cache.Get(ctx, "cron")
	assert.NoError(t, err)
	assert.Equal(t, "model-cron", result)
	assert.Equal(t, int32(2), atomic.LoadInt32(&fetchCount), "should fetch again after expiry")
}

func TestStaticCache_Get_Concurrent(t *testing.T) {
	cache := NewStaticCache()
	cache.SetRefreshInterval(10 * time.Millisecond)

	var fetchCount int32
	cache.fetchFunc = func(ctx context.Context, contextType string) (interface{}, error) {
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&fetchCount, 1)
		return "model-" + contextType, nil
	}

	ctx := context.Background()
	var wg sync.WaitGroup
	var results [20]interface{}
	var errors [20]error

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errors[idx] = cache.Get(ctx, "concurrent")
		}(i)
	}

	wg.Wait()

	for i := 0; i < 20; i++ {
		assert.NoError(t, errors[i])
		assert.Equal(t, "model-concurrent", results[i])
	}

	assert.LessOrEqual(t, atomic.LoadInt32(&fetchCount), int32(5), "should deduplicate concurrent fetches")
}

func TestStaticCache_Refresh(t *testing.T) {
	cache := NewStaticCache()
	cache.SetRefreshInterval(1 * time.Hour)

	var fetchCount int32
	cache.fetchFunc = func(ctx context.Context, contextType string) (interface{}, error) {
		atomic.AddInt32(&fetchCount, 1)
		return "refreshed-" + contextType, nil
	}

	ctx := context.Background()

	_, err := cache.Get(ctx, "heartbeat")
	assert.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&fetchCount))

	err = cache.Refresh(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int32(4), atomic.LoadInt32(&fetchCount), "should fetch all contexts on refresh")

	result, err := cache.Get(ctx, "heartbeat")
	assert.NoError(t, err)
	assert.Equal(t, "refreshed-heartbeat", result)
}

func TestStaticCache_Get_DifferentContexts(t *testing.T) {
	cache := NewStaticCache()
	cache.SetRefreshInterval(1 * time.Hour)

	cache.fetchFunc = func(ctx context.Context, contextType string) (interface{}, error) {
		return "model-for-" + contextType, nil
	}

	ctx := context.Background()

	cronResult, err := cache.Get(ctx, "cron")
	assert.NoError(t, err)
	assert.Equal(t, "model-for-cron", cronResult)

	heartbeatResult, err := cache.Get(ctx, "heartbeat")
	assert.NoError(t, err)
	assert.Equal(t, "model-for-heartbeat", heartbeatResult)

	webhookResult, err := cache.Get(ctx, "webhook")
	assert.NoError(t, err)
	assert.Equal(t, "model-for-webhook", webhookResult)

	assert.NotEqual(t, cronResult, heartbeatResult)
	assert.NotEqual(t, heartbeatResult, webhookResult)
}

func TestStaticCache_Get_ContextTimeout(t *testing.T) {
	cache := NewStaticCache()
	cache.SetRefreshInterval(1 * time.Hour)

	cache.fetchFunc = func(ctx context.Context, contextType string) (interface{}, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
			return "slow-model", nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := cache.Get(ctx, "timeout")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deadline")
}

func TestStaticCache_Refresh_Error(t *testing.T) {
	cache := NewStaticCache()
	cache.SetRefreshInterval(1 * time.Hour)

	cache.fetchFunc = func(ctx context.Context, contextType string) (interface{}, error) {
		return nil, assert.AnError
	}

	ctx := context.Background()

	err := cache.Refresh(ctx)
	assert.Error(t, err)
}

func TestStaticCache_Get_UseCachedOnError(t *testing.T) {
	cache := NewStaticCache()
	cache.SetRefreshInterval(10 * time.Millisecond)

	callCount := 0
	cache.fetchFunc = func(ctx context.Context, contextType string) (interface{}, error) {
		callCount++
		if callCount == 1 {
			return "first-result", nil
		}
		return nil, assert.AnError
	}

	ctx := context.Background()

	result, err := cache.Get(ctx, "error-test")
	assert.NoError(t, err)
	assert.Equal(t, "first-result", result)

	time.Sleep(15 * time.Millisecond)

	result, err = cache.Get(ctx, "error-test")
	assert.Error(t, err, "should return error on refresh failure")
	assert.Nil(t, result)
}

func TestStaticCache_KeyFormat(t *testing.T) {
	cache := NewStaticCache()

	tests := []struct {
		context  string
		expected string
	}{
		{"cron", "static_recommendation:cron"},
		{"heartbeat", "static_recommendation:heartbeat"},
		{"webhook", "static_recommendation:webhook"},
	}

	for _, tt := range tests {
		t.Run(tt.context, func(t *testing.T) {
			key := cache.cacheKey(tt.context)
			assert.Equal(t, tt.expected, key)
		})
	}
}

func TestStaticCache_SetRefreshInterval(t *testing.T) {
	cache := NewStaticCache()

	cache.SetRefreshInterval(5 * time.Minute)
	assert.Equal(t, 5*time.Minute, cache.refreshInterval)

	cache.SetRefreshInterval(2 * time.Hour)
	assert.Equal(t, 2*time.Hour, cache.refreshInterval)
}

func TestStaticCache_DefaultFetch(t *testing.T) {
	cache := NewStaticCache()

	ctx := context.Background()

	result, err := cache.Get(ctx, "test")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}
