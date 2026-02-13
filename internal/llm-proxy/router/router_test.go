// Package router provides intelligent model selection using LLM-based routing.
package router

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/llm-proxy"
)

func TestRouterRequest(t *testing.T) {
	req := RouterRequest{
		UserQuery: "Write hello world in python",
		UserContext: proxy.UserContext{
			UserID:                "user-123",
			PlanTier:              "basic",
			QuotaRemainingPercent: 70,
		},
		AvailableModels: []string{"gpt-4o-mini", "claude-3-5-haiku"},
	}

	if req.UserQuery != "Write hello world in python" {
		t.Error("UserQuery not set correctly")
	}
}

func TestRouterResponse(t *testing.T) {
	resp := RouterResponse{
		SelectedModel: "gpt-4o-mini",
		Reasoning:     "Simple query, economy tier appropriate",
		Confidence:    "high",
		EstimatedTokens: proxy.TokenEstimate{
			Input:  100,
			Output: 50,
		},
	}

	if resp.SelectedModel != "gpt-4o-mini" {
		t.Error("SelectedModel not set correctly")
	}
}

func TestRouterAttempt(t *testing.T) {
	attempt := RouterAttempt{
		Model: "gpt-4o",
		Error: "rate limit exceeded",
	}

	if attempt.Model != "gpt-4o" {
		t.Error("Model not set correctly")
	}
	if attempt.Error != "rate limit exceeded" {
		t.Error("Error not set correctly")
	}
}

func TestBuildRouterPrompt(t *testing.T) {
	req := RouterRequest{
		UserQuery: "Write hello world in python",
		UserContext: proxy.UserContext{
			PlanTier:              "basic",
			QuotaRemainingPercent: 70,
		},
		AvailableModels: []string{"gpt-4o-mini", "claude-3-5-haiku"},
		ModelCosts: map[string]proxy.CostInfo{
			"gpt-4o-mini": {
				InputCostPerMillion:  0.15,
				OutputCostPerMillion: 0.60,
			},
		},
	}

	prompt := BuildRouterPrompt(req)

	if prompt == "" {
		t.Error("Prompt should not be empty")
	}

	// Check that key elements are in the prompt
	if !contains(prompt, "Write hello world in python") {
		t.Error("User query not in prompt")
	}
	if !contains(prompt, "basic") {
		t.Error("Plan tier not in prompt")
	}
}

func TestSelectModel(t *testing.T) {
	// This test will fail until we implement SelectModel
	ctx := context.Background()

	req := RouterRequest{
		UserQuery: "Write hello world in python",
		UserContext: proxy.UserContext{
			PlanTier:              "basic",
			QuotaRemainingPercent: 70,
		},
		AvailableModels: []string{"gpt-4o-mini"},
	}

	// This will fail because SelectModel is not yet implemented
	_, err := SelectModel(ctx, nil, req)
	if err == nil {
		t.Error("Expected error when LLM client is nil")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
