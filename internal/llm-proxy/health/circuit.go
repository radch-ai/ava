package health

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Service provides health tracking operations with Redis backend.
type Service struct {
	store *Store
}

// NewService creates a new health service.
func NewService(client *redis.Client, config *CircuitBreakerConfig) *Service {
	return &Service{
		store: NewStore(client, config),
	}
}

// GetAllHealth retrieves health status for all provider:models.
func GetAllHealth(ctx context.Context, client *redis.Client) (map[string]*ProviderHealth, error) {
	store := NewStore(client, DefaultCircuitBreakerConfig())
	return store.GetAll(ctx)
}

// ResetHealth resets health status for a provider:model to healthy.
func ResetHealth(ctx context.Context, client *redis.Client, provider, model string) error {
	store := NewStore(client, DefaultCircuitBreakerConfig())

	health := &ProviderHealth{
		Status:              StatusHealthy,
		FailureCount:        0,
		SuccessCount:        0,
		ConsecutiveFailures: 0,
		LastFailure:         time.Time{},
		LastSuccess:         time.Time{},
		CircuitOpenUntil:    time.Time{},
	}

	return store.Save(ctx, provider, model, health)
}

// GetUnhealthyModels returns all models that are currently unhealthy.
func GetUnhealthyModels(ctx context.Context, client *redis.Client) ([]string, error) {
	store := NewStore(client, DefaultCircuitBreakerConfig())

	all, err := store.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	var unhealthy []string
	for key, health := range all {
		if health.Status == StatusUnhealthy {
			unhealthy = append(unhealthy, key)
		}
	}

	return unhealthy, nil
}

// GetDegradedModels returns all models that are currently degraded.
func GetDegradedModels(ctx context.Context, client *redis.Client) ([]string, error) {
	store := NewStore(client, DefaultCircuitBreakerConfig())

	all, err := store.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	var degraded []string
	for key, health := range all {
		if health.Status == StatusDegraded {
			degraded = append(degraded, key)
		}
	}

	return degraded, nil
}

// ShouldRateLimit returns true if a model should be rate limited due to health issues.
func ShouldRateLimit(ctx context.Context, client *redis.Client, provider, model string) (bool, error) {
	store := NewStore(client, DefaultCircuitBreakerConfig())

	health, err := store.Get(ctx, provider, model)
	if err != nil {
		return false, err
	}

	// Rate limit unhealthy models
	return health.Status == StatusUnhealthy, nil
}

// ConsecutiveSuccessCount returns the number of consecutive successes.
// This is derived from the state transitions.
func (h *ProviderHealth) ConsecutiveSuccessCount() int {
	// Since we reset consecutive failures on success, this is implicit
	// A better approach would be to track this explicitly, but for now
	// we can infer it from the status change
	if h.Status == StatusHealthy {
		return 1
	}
	return 0
}

// RecordSuccess records a successful request to provider:model.
func RecordSuccess(ctx context.Context, client *redis.Client, provider, model string) error {
	store := NewStore(client, DefaultCircuitBreakerConfig())

	// Get current health
	health, err := store.Get(ctx, provider, model)
	if err != nil {
		return err
	}

	// Update success fields
	health.SuccessCount++
	health.LastSuccess = time.Now()
	health.ConsecutiveFailures = 0

	// Update status based on state
	// If we were unhealthy or degraded, a single success should bring us to healthy
	// This is the expected behavior for circuit breaker recovery
	if health.Status == StatusUnhealthy || health.Status == StatusDegraded {
		health.Status = StatusHealthy
		health.CircuitOpenUntil = time.Time{}
	}

	return store.Save(ctx, provider, model, health)
}

// RecordFailure records a failed request to provider:model.
func RecordFailure(ctx context.Context, client *redis.Client, provider, model string) error {
	store := NewStore(client, DefaultCircuitBreakerConfig())

	// Get current health
	health, err := store.Get(ctx, provider, model)
	if err != nil {
		return err
	}

	config := DefaultCircuitBreakerConfig()

	// Update failure fields
	health.FailureCount++
	health.ConsecutiveFailures++
	health.LastFailure = time.Now()

	// Update status based on consecutive failures
	switch {
	case health.ConsecutiveFailures >= config.FailureThreshold:
		health.Status = StatusUnhealthy
		health.CircuitOpenUntil = time.Now().Add(config.RecoveryTimeout)
	case health.ConsecutiveFailures >= config.DegradedThreshold:
		health.Status = StatusDegraded
	default:
		health.Status = StatusHealthy
	}

	return store.Save(ctx, provider, model, health)
}

// GetHealth retrieves health data for a provider:model.
func GetHealth(ctx context.Context, client *redis.Client, provider, model string) (*ProviderHealth, error) {
	store := NewStore(client, DefaultCircuitBreakerConfig())
	return store.Get(ctx, provider, model)
}

// IsHealthy checks if a provider:model is healthy.
func IsHealthy(ctx context.Context, client *redis.Client, provider, model string) (bool, error) {
	health, err := GetHealth(ctx, client, provider, model)
	if err != nil {
		return false, err
	}

	// Check if circuit is open
	if !health.CircuitOpenUntil.IsZero() && time.Now().Before(health.CircuitOpenUntil) {
		return false, nil
	}

	return health.Status == StatusHealthy, nil
}
