package health

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// mockRedis creates a mock redis server for testing
func mockRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	s, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(&redis.Options{
		Addr: s.Addr(),
	})
	return s, client
}

func TestRecordSuccess(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Test: Record a successful request
	err := RecordSuccess(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("RecordSuccess failed: %v", err)
	}

	// Verify: Check health status
	health, err := GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != StatusHealthy {
		t.Errorf("Expected status %s, got %s", StatusHealthy, health.Status)
	}

	if health.ConsecutiveFailures != 0 {
		t.Errorf("Expected 0 consecutive failures, got %d", health.ConsecutiveFailures)
	}
}

func TestRecordFailure(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Test: Record a failure
	err := RecordFailure(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("RecordFailure failed: %v", err)
	}

	// Verify: Check health status
	health, err := GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.ConsecutiveFailures != 1 {
		t.Errorf("Expected 1 consecutive failure, got %d", health.ConsecutiveFailures)
	}

	if health.FailureCount == 0 {
		t.Error("Expected failure count > 0")
	}
}

func TestCircuitBreaker(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Record 3 failures (threshold is 3)
	for i := 0; i < 3; i++ {
		_ = RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Check if circuit is open
	health, err := GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != StatusUnhealthy {
		t.Errorf("Expected status %s, got %s", StatusUnhealthy, health.Status)
	}

	if health.CircuitOpenUntil.IsZero() {
		t.Error("Expected circuit to be open (CircuitOpenUntil should be set)")
	}
}

func TestIsHealthy(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Test: Initially healthy
	healthy, err := IsHealthy(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("IsHealthy failed: %v", err)
	}

	if !healthy {
		t.Error("Expected initially healthy")
	}

	// Record failures to trip circuit breaker
	for i := 0; i < 3; i++ {
		_ = RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Check if unhealthy now
	healthy, err = IsHealthy(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("IsHealthy failed: %v", err)
	}

	if healthy {
		t.Error("Expected unhealthy after failures")
	}
}

func TestCircuitBreakerRecovery(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Trip circuit breaker with failures
	for i := 0; i < 3; i++ {
		_ = RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Verify circuit is open
	health, _ := GetHealth(ctx, client, "openai", "gpt-4o")
	if health.Status != StatusUnhealthy {
		t.Error("Expected circuit to be open")
	}

	// Record 2 successes to recover (success threshold is 2)
	_ = RecordSuccess(ctx, client, "openai", "gpt-4o")
	_ = RecordSuccess(ctx, client, "openai", "gpt-4o")

	// Verify circuit is closed again
	health, err := GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != StatusHealthy {
		t.Errorf("Expected status %s after recovery, got %s", StatusHealthy, health.Status)
	}

	if health.ConsecutiveFailures != 0 {
		t.Errorf("Expected consecutive failures reset, got %d", health.ConsecutiveFailures)
	}
}

func TestGetAllHealth(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: Record some health data for different models
	_ = RecordFailure(ctx, client, "openai", "gpt-4o")
	_ = RecordSuccess(ctx, client, "anthropic", "claude-3-5-sonnet")
	_ = RecordFailure(ctx, client, "google", "gemini-2.0-flash")
	_ = RecordFailure(ctx, client, "google", "gemini-2.0-flash")

	// Get all health
	all, err := GetAllHealth(ctx, client)
	if err != nil {
		t.Fatalf("GetAllHealth failed: %v", err)
	}

	if len(all) == 0 {
		t.Error("Expected some health data")
	}

	// Verify specific models exist (keys are prefixed with "health:")
	key := "health:openai:gpt-4o"
	if _, exists := all[key]; !exists {
		t.Errorf("Expected %s to exist", key)
	}
}

func TestDegradedStatus(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Record 2 failures (below threshold but should show degraded)
	for i := 0; i < 2; i++ {
		_ = RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Check status
	health, err := GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != StatusDegraded {
		t.Errorf("Expected status %s, got %s", StatusDegraded, health.Status)
	}
}

func TestResetHealth(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: Record some failures
	for i := 0; i < 2; i++ {
		_ = RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Verify degraded state
	health, _ := GetHealth(ctx, client, "openai", "gpt-4o")
	if health.Status != StatusDegraded {
		t.Error("Expected degraded before reset")
	}

	// Reset health
	err := ResetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("ResetHealth failed: %v", err)
	}

	// Verify healthy state after reset
	health, err = GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != StatusHealthy {
		t.Errorf("Expected status %s after reset, got %s", StatusHealthy, health.Status)
	}

	if health.FailureCount != 0 {
		t.Errorf("Expected failure count reset, got %d", health.FailureCount)
	}
}
