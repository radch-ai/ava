package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type StaticCache struct {
	store           map[string]*cacheEntry
	mu              sync.RWMutex
	refreshInterval time.Duration
	fetchFunc       FetchFunc
	sf              singleflight.Group
}

type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

type FetchFunc func(ctx context.Context, contextType string) (interface{}, error)

func NewStaticCache() *StaticCache {
	return &StaticCache{
		store: make(map[string]*cacheEntry),
	}
}

func (c *StaticCache) SetRefreshInterval(interval time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshInterval = interval
}

func (c *StaticCache) Get(ctx context.Context, contextType string) (interface{}, error) {
	if c.fetchFunc == nil {
		return nil, fmt.Errorf("fetch function not configured")
	}

	key := c.cacheKey(contextType)

	c.mu.RLock()
	entry, exists := c.store[key]
	if exists && time.Now().Before(entry.expiresAt) {
		c.mu.RUnlock()
		return entry.value, nil
	}
	c.mu.RUnlock()

	return c.refreshSingle(ctx, contextType, key, false)
}

func (c *StaticCache) Refresh(ctx context.Context) error {
	contexts := []string{"cron", "heartbeat", "webhook"}

	var wg sync.WaitGroup
	errChan := make(chan error, len(contexts))

	for _, contextType := range contexts {
		wg.Add(1)
		go func(ct string) {
			defer wg.Done()
			key := c.cacheKey(ct)
			if _, err := c.refreshSingle(ctx, ct, key, true); err != nil {
				errChan <- fmt.Errorf("failed to refresh %s: %w", ct, err)
			}
		}(contextType)
	}

	wg.Wait()
	close(errChan)

	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during refresh", len(errors))
	}

	return nil
}

func (c *StaticCache) cacheKey(contextType string) string {
	return fmt.Sprintf("static_recommendation:%s", contextType)
}

func (c *StaticCache) refreshSingle(ctx context.Context, contextType string, key string, forceRefresh bool) (interface{}, error) {
	result, err, _ := c.sf.Do(key, func() (interface{}, error) {
		c.mu.Lock()
		entry, exists := c.store[key]
		if exists && !forceRefresh && time.Now().Before(entry.expiresAt) {
			value := entry.value
			c.mu.Unlock()
			return value, nil
		}
		c.mu.Unlock()

		if c.fetchFunc == nil {
			return nil, fmt.Errorf("fetch function not configured")
		}

		value, err := c.fetchFunc(ctx, contextType)
		if err != nil {
			c.mu.RLock()
			entry, exists := c.store[key]
			if exists && time.Now().Before(entry.expiresAt) {
				value = entry.value
				c.mu.RUnlock()
				return value, fmt.Errorf("refresh failed, using cached value: %w", err)
			}
			c.mu.RUnlock()
			return nil, err
		}

		expiresAt := time.Now().Add(c.refreshInterval)

		c.mu.Lock()
		c.store[key] = &cacheEntry{
			value:     value,
			expiresAt: expiresAt,
		}
		c.mu.Unlock()

		return value, nil
	})

	return result, err
}
