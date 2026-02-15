// Package proxy_test provides tests for proxy types and constants.
package proxy_test

import (
	"testing"

	llm_proxy "github.com/nevaria-ai/gateway/internal/llm-proxy"
)

func TestUsageEvent(t *testing.T) {
	event := llm_proxy.UsageEvent{
		UserID:       "test-user",
		Model:        "gpt-4o-mini",
		Provider:     "openai",
		TokensInput:  100,
		TokensOutput: 50,
		CostTotal:    0.0003,
		ChargedFrom:  "plan",
	}

	if event.UserID != "test-user" {
		t.Error("UserID not set correctly")
	}

	if event.Model != "gpt-4o-mini" {
		t.Error("Model not set correctly")
	}

	if event.Provider != "openai" {
		t.Error("Provider not set correctly")
	}

	if event.TokensInput != 100 {
		t.Error("TokensInput not set correctly")
	}

	if event.TokensOutput != 50 {
		t.Error("TokensOutput not set correctly")
	}

	if event.CostTotal != 0.0003 {
		t.Error("CostTotal not set correctly")
	}

	if event.ChargedFrom != "plan" {
		t.Error("ChargedFrom not set correctly")
	}
}

func TestUserContext(t *testing.T) {
	ctx := llm_proxy.UserContext{
		UserID:                "user-123",
		PlanTier:              "premium",
		QuotaRemaining:        10.50,
		QuotaRemainingPercent: 50,
		HasAddonCredits:       true,
		AddonRemaining:        5.00,
		DaysUntilReset:        15,
		IsUsingAddon:          false,
	}

	if ctx.UserID != "user-123" {
		t.Error("UserID not set correctly")
	}

	if ctx.PlanTier != "premium" {
		t.Error("PlanTier not set correctly")
	}

	if ctx.QuotaRemaining != 10.50 {
		t.Error("QuotaRemaining not set correctly")
	}

	if ctx.HasAddonCredits != true {
		t.Error("HasAddonCredits not set correctly")
	}
}

func TestProviderHealth(t *testing.T) {
	health := llm_proxy.ProviderHealth{
		Status:              "healthy",
		FailureCount:        0,
		ConsecutiveFailures: 0,
	}

	if health.Status != "healthy" {
		t.Error("Status not set correctly")
	}

	if health.FailureCount != 0 {
		t.Error("FailureCount not set correctly")
	}
}

func TestGatewayStatus(t *testing.T) {
	if llm_proxy.StatusHealthy != "healthy" {
		t.Error("StatusHealthy constant incorrect")
	}

	if llm_proxy.StatusDegraded != "degraded" {
		t.Error("StatusDegraded constant incorrect")
	}

	if llm_proxy.StatusCritical != "critical" {
		t.Error("StatusCritical constant incorrect")
	}

	if llm_proxy.StatusPaused != "paused" {
		t.Error("StatusPaused constant incorrect")
	}

	status := llm_proxy.StatusHealthy
	if status != "healthy" {
		t.Error("GatewayStatus type not working correctly")
	}
}
