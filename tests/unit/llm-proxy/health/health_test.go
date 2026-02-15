package health_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	llm_proxy_health "github.com/nevaria-ai/gateway/internal/llm-proxy/health"
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
	err := llm_proxy_health.RecordSuccess(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("RecordSuccess failed: %v", err)
	}

	// Verify: Check health status
	health, err := llm_proxy_health.GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != llm_proxy_health.StatusHealthy {
		t.Errorf("Expected status %s, got %s", llm_proxy_health.StatusHealthy, health.Status)
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
	err := llm_proxy_health.RecordFailure(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("RecordFailure failed: %v", err)
	}

	// Verify: Check health status
	health, err := llm_proxy_health.GetHealth(ctx, client, "openai", "gpt-4o")
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
		_ = llm_proxy_health.RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Check if circuit is open
	health, err := llm_proxy_health.GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != llm_proxy_health.StatusUnhealthy {
		t.Errorf("Expected status %s, got %s", llm_proxy_health.StatusUnhealthy, health.Status)
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
	healthy, err := llm_proxy_health.IsHealthy(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("IsHealthy failed: %v", err)
	}

	if !healthy {
		t.Error("Expected initially healthy")
	}

	// Record failures to trip circuit breaker
	for i := 0; i < 3; i++ {
		_ = llm_proxy_health.RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Check if unhealthy now
	healthy, err = llm_proxy_health.IsHealthy(ctx, client, "openai", "gpt-4o")
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
		_ = llm_proxy_health.RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Verify circuit is open
	health, _ := llm_proxy_health.GetHealth(ctx, client, "openai", "gpt-4o")
	if health.Status != llm_proxy_health.StatusUnhealthy {
		t.Error("Expected circuit to be open")
	}

	// Record 2 successes to recover (success threshold is 2)
	_ = llm_proxy_health.RecordSuccess(ctx, client, "openai", "gpt-4o")
	_ = llm_proxy_health.RecordSuccess(ctx, client, "openai", "gpt-4o")

	// Verify circuit is closed again
	health, err := llm_proxy_health.GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != llm_proxy_health.StatusHealthy {
		t.Errorf("Expected status %s after recovery, got %s", llm_proxy_health.StatusHealthy, health.Status)
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
	_ = llm_proxy_health.RecordFailure(ctx, client, "openai", "gpt-4o")
	_ = llm_proxy_health.RecordSuccess(ctx, client, "anthropic", "claude-3-5-sonnet")
	_ = llm_proxy_health.RecordFailure(ctx, client, "google", "gemini-2.0-flash")
	_ = llm_proxy_health.RecordFailure(ctx, client, "google", "gemini-2.0-flash")

	// Get all health
	all, err := llm_proxy_health.GetAllHealth(ctx, client)
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
		_ = llm_proxy_health.RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Check status
	health, err := llm_proxy_health.GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != llm_proxy_health.StatusDegraded {
		t.Errorf("Expected status %s, got %s", llm_proxy_health.StatusDegraded, health.Status)
	}
}

func TestResetHealth(t *testing.T) {
	s, client := mockRedis(t)
	defer s.Close()

	ctx := context.Background()

	// Setup: Record some failures
	for i := 0; i < 2; i++ {
		_ = llm_proxy_health.RecordFailure(ctx, client, "openai", "gpt-4o")
	}

	// Verify degraded state
	health, _ := llm_proxy_health.GetHealth(ctx, client, "openai", "gpt-4o")
	if health.Status != llm_proxy_health.StatusDegraded {
		t.Error("Expected degraded before reset")
	}

	// Reset health
	err := llm_proxy_health.ResetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("ResetHealth failed: %v", err)
	}

	// Verify healthy state after reset
	health, err = llm_proxy_health.GetHealth(ctx, client, "openai", "gpt-4o")
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.Status != llm_proxy_health.StatusHealthy {
		t.Errorf("Expected status %s after reset, got %s", llm_proxy_health.StatusHealthy, health.Status)
	}

	if health.FailureCount != 0 {
		t.Errorf("Expected failure count reset, got %d", health.FailureCount)
	}
}
