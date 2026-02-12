package health

import "time"

// HealthStatus represents the health status of a provider:model.
type HealthStatus string

const (
	StatusHealthy   HealthStatus = "healthy"
	StatusDegraded  HealthStatus = "degraded"
	StatusUnhealthy HealthStatus = "unhealthy"
)

// ProviderHealth tracks the health status of a provider:model.
type ProviderHealth struct {
	Status              HealthStatus `json:"status"`
	FailureCount        int          `json:"failure_count"`
	SuccessCount        int          `json:"success_count"`
	LastFailure         time.Time    `json:"last_failure,omitempty"`
	LastSuccess         time.Time    `json:"last_success,omitempty"`
	ConsecutiveFailures int          `json:"consecutive_failures"`
	CircuitOpenUntil    time.Time    `json:"circuit_open_until,omitempty"`
}

// CircuitBreakerConfig configures circuit breaker behavior.
type CircuitBreakerConfig struct {
	FailureThreshold  int           `json:"failure_threshold"`  // Number of failures to trip
	RecoveryTimeout   time.Duration `json:"recovery_timeout"`   // Time to wait before attempting recovery
	SuccessThreshold  int           `json:"success_threshold"`  // Number of successes to close circuit
	DegradedThreshold int           `json:"degraded_threshold"` // Failures to mark as degraded
}

// DefaultCircuitBreakerConfig returns default circuit breaker configuration.
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold:  3,
		RecoveryTimeout:   60 * time.Second,
		SuccessThreshold:  2,
		DegradedThreshold: 2,
	}
}
