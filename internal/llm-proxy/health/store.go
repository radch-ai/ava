package health

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store handles health data persistence with Redis.
type Store struct {
	client *redis.Client
	config *CircuitBreakerConfig
}

// NewStore creates a new health store.
func NewStore(client *redis.Client, config *CircuitBreakerConfig) *Store {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}
	return &Store{
		client: client,
		config: config,
	}
}

// healthKey generates the Redis key for health data.
func healthKey(provider, model string) string {
	return "health:" + provider + ":" + model
}

// Get retrieves health data for a provider:model.
func (s *Store) Get(ctx context.Context, provider, model string) (*ProviderHealth, error) {
	key := healthKey(provider, model)
	data, err := s.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			// Return default healthy state if not exists
			return &ProviderHealth{
				Status:              StatusHealthy,
				FailureCount:        0,
				SuccessCount:        0,
				ConsecutiveFailures: 0,
			}, nil
		}
		return nil, fmt.Errorf("failed to get health: %w", err)
	}

	var health ProviderHealth
	if err := json.Unmarshal([]byte(data), &health); err != nil {
		return nil, fmt.Errorf("failed to unmarshal health: %w", err)
	}

	// Check if circuit should be closed (recovery timeout elapsed)
	if !health.CircuitOpenUntil.IsZero() && time.Now().After(health.CircuitOpenUntil) {
		health.CircuitOpenUntil = time.Time{}
		health.Status = StatusDegraded // Move to degraded state for recovery
		// Don't auto-save here - will be updated on next operation
	}

	return &health, nil
}

// Save stores health data for a provider:model.
func (s *Store) Save(ctx context.Context, provider, model string, health *ProviderHealth) error {
	key := healthKey(provider, model)
	data, err := json.Marshal(health)
	if err != nil {
		return fmt.Errorf("failed to marshal health: %w", err)
	}

	// Set with expiration (24 hours)
	return s.client.Set(ctx, key, data, 24*time.Hour).Err()
}

// Delete removes health data for a provider:model.
func (s *Store) Delete(ctx context.Context, provider, model string) error {
	key := healthKey(provider, model)
	return s.client.Del(ctx, key).Err()
}

// GetAll retrieves all health data.
func (s *Store) GetAll(ctx context.Context) (map[string]*ProviderHealth, error) {
	// Scan all health keys
	pattern := "health:*"
	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to scan keys: %w", err)
	}

	result := make(map[string]*ProviderHealth)

	for _, key := range keys {
		data, err := s.client.Get(ctx, key).Result()
		if err != nil {
			continue // Skip keys that may have expired
		}

		var health ProviderHealth
		if err := json.Unmarshal([]byte(data), &health); err != nil {
			continue
		}

		result[key] = &health
	}

	return result, nil
}
