package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type SelectorCache struct {
	client *redis.Client
}

func NewSelectorCache(client *redis.Client) *SelectorCache {
	return &SelectorCache{
		client: client,
	}
}

func (sc *SelectorCache) Get(ctx context.Context, userID string, sessionID string, tier string) (*ModelRecommendation, error) {
	key := sc.cacheKey(userID, sessionID)

	val, err := sc.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("cache miss: recommendation not found")
		}
		return nil, fmt.Errorf("failed to get from cache: %w", err)
	}

	var recommendation ModelRecommendation
	if err := json.Unmarshal([]byte(val), &recommendation); err != nil {
		return nil, fmt.Errorf("failed to unmarshal recommendation: %w", err)
	}

	return &recommendation, nil
}

func (sc *SelectorCache) Set(ctx context.Context, userID string, sessionID string, tier string, recommendation *ModelRecommendation) error {
	key := sc.cacheKey(userID, sessionID)
	ttl := sc.getTTL(tier)

	data, err := json.Marshal(recommendation)
	if err != nil {
		return fmt.Errorf("failed to marshal recommendation: %w", err)
	}

	if err := sc.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set cache: %w", err)
	}

	return nil
}

func (sc *SelectorCache) cacheKey(userID string, sessionID string) string {
	return fmt.Sprintf("nd_recommendation:%s:%s", userID, sessionID)
}

func (sc *SelectorCache) getTTL(tier string) time.Duration {
	switch tier {
	case "pro":
		return 15 * time.Minute
	case "standard":
		return 30 * time.Minute
	case "lite":
		return 60 * time.Minute
	default:
		return 30 * time.Minute
	}
}
