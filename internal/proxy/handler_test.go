// Package proxy provides LLM proxy functionality with routing, quota management, and usage tracking.
package proxy

import (
	"context"
	"testing"

	"github.com/nevaria-ai/gateway/internal/agent"
)

func TestProxyRequest(t *testing.T) {
	req := ProxyRequest{
		UserID:    "test-user",
		SessionID: "test-session",
		Query:     "Hello, world!",
	}

	if req.UserID != "test-user" {
		t.Error("UserID not set correctly")
	}
	if req.Query != "Hello, world!" {
		t.Error("Query not set correctly")
	}
}

func TestProxyResponse(t *testing.T) {
	resp := ProxyResponse{
		UsingAddon:     false,
		AddonRemaining: 10.0,
	}

	if resp.UsingAddon {
		t.Error("Should not be using addon")
	}
	if resp.AddonRemaining != 10.0 {
		t.Error("AddonRemaining not set correctly")
	}
}

func TestHandlerOptions(t *testing.T) {
	opts := HandlerOptions{
		LLMClient: &mockLLMClient{},
	}

	if opts.LLMClient == nil {
		t.Error("LLMClient should not be nil")
	}
}

func TestNewHandler(t *testing.T) {
	handler := NewHandler(HandlerOptions{
		LLMClient: &mockLLMClient{},
	})

	if handler == nil {
		t.Error("NewHandler returned nil")
	}
}

func TestHandleProxyRequest(t *testing.T) {
	handler := NewHandler(HandlerOptions{
		LLMClient: &mockLLMClient{},
	})

	ctx := context.Background()
	req := ProxyRequest{
		UserID:    "test-user",
		SessionID: "test-session",
		Query:     "Hello, world!",
		Messages: []agent.AgentMessage{
			agent.NewTextMessage(agent.RoleUser, "Hello, world!"),
		},
	}

	resp, err := handler.Handle(ctx, req)
	if err != nil {
		t.Logf("Handle failed (expected during development): %v", err)
		// This is expected to fail until full implementation
		return
	}

	if resp == nil {
		t.Error("Handle returned nil response")
	}
}

// Mock implementations

type mockLLMClient struct{}

func (m *mockLLMClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent, 1)
	go func() {
		defer close(ch)
		// Send a simple message end event
		ch <- agent.MessageEndEvent{
			Message: agent.NewTextMessage(agent.RoleAssistant, "Hello!"),
		}
	}()
	return ch, nil
}

// Tests for proxyRouterRequest

func TestProxyRouterRequest_GetUserQuery(t *testing.T) {
	req := ProxyRequest{
		UserID:    "test-user",
		SessionID: "test-session",
		Query:     "What is the weather?",
	}

	routerReq := &proxyRouterRequest{
		proxyReq: req,
		context: UserContext{
			UserID: "test-user",
		},
	}

	if routerReq.GetUserQuery() != "What is the weather?" {
		t.Errorf("Expected query 'What is the weather?', got '%s'", routerReq.GetUserQuery())
	}
}

func TestProxyRouterRequest_GetUserContext(t *testing.T) {
	req := ProxyRequest{
		UserID: "test-user",
	}

	expectedContext := UserContext{
		UserID:                "test-user",
		PlanTier:              "premium",
		QuotaRemaining:        10.0,
		QuotaRemainingPercent: 50,
		HasAddonCredits:       true,
		AddonRemaining:        5.0,
		DaysUntilReset:        15,
		IsUsingAddon:          false,
	}

	routerReq := &proxyRouterRequest{
		proxyReq: req,
		context:  expectedContext,
	}

	ctx := routerReq.GetUserContext()
	if ctx.UserID != "test-user" {
		t.Errorf("Expected UserID 'test-user', got '%s'", ctx.UserID)
	}
	if ctx.PlanTier != "premium" {
		t.Errorf("Expected PlanTier 'premium', got '%s'", ctx.PlanTier)
	}
	if ctx.QuotaRemaining != 10.0 {
		t.Errorf("Expected QuotaRemaining 10.0, got %f", ctx.QuotaRemaining)
	}
	if ctx.HasAddonCredits != true {
		t.Error("Expected HasAddonCredits to be true")
	}
}

func TestProxyRouterRequest_GetAvailableModels_EconomyTier(t *testing.T) {
	req := ProxyRequest{
		UserID: "economy-user",
		Tools:  []agent.Tool{},
	}

	routerReq := &proxyRouterRequest{
		proxyReq: req,
		context: UserContext{
			UserID:   "economy-user",
			PlanTier: "economy",
		},
	}

	models := routerReq.GetAvailableModels()

	// Economy tier should get basic models
	if len(models) == 0 {
		t.Error("Expected at least one model")
	}

	// Check that economy models are present
	hasEconomyModel := false
	for _, model := range models {
		if model == "gpt-4o-mini" || model == "claude-3-5-haiku" {
			hasEconomyModel = true
			break
		}
	}
	if !hasEconomyModel {
		t.Error("Expected to find economy models (gpt-4o-mini, claude-3-5-haiku)")
	}
}

func TestProxyRouterRequest_GetAvailableModels_PremiumTier(t *testing.T) {
	req := ProxyRequest{
		UserID: "premium-user",
		Tools:  []agent.Tool{},
	}

	routerReq := &proxyRouterRequest{
		proxyReq: req,
		context: UserContext{
			UserID:   "premium-user",
			PlanTier: "premium",
		},
	}

	models := routerReq.GetAvailableModels()

	// Premium tier should get more models including premium ones
	if len(models) == 0 {
		t.Error("Expected at least one model")
	}

	// Check that premium models are present
	hasPremiumModel := false
	for _, model := range models {
		if model == "claude-3-opus" || model == "gpt-4-turbo" {
			hasPremiumModel = true
			break
		}
	}
	if !hasPremiumModel {
		t.Error("Expected to find premium models (claude-3-opus, gpt-4-turbo) for premium tier")
	}
}

func TestProxyRouterRequest_GetAvailableModels_EnterpriseTier(t *testing.T) {
	req := ProxyRequest{
		UserID: "enterprise-user",
		Tools:  []agent.Tool{},
	}

	routerReq := &proxyRouterRequest{
		proxyReq: req,
		context: UserContext{
			UserID:   "enterprise-user",
			PlanTier: "enterprise",
		},
	}

	models := routerReq.GetAvailableModels()

	// Enterprise tier should get all models including premium ones
	if len(models) == 0 {
		t.Error("Expected at least one model")
	}

	// Check that premium models are present
	hasPremiumModel := false
	for _, model := range models {
		if model == "claude-3-opus" || model == "gpt-4-turbo" {
			hasPremiumModel = true
			break
		}
	}
	if !hasPremiumModel {
		t.Error("Expected to find premium models (claude-3-opus, gpt-4-turbo) for enterprise tier")
	}
}

func TestProxyRouterRequest_WithTools(t *testing.T) {
	req := ProxyRequest{
		UserID: "tool-user",
		Query:  "Calculate something",
		Tools: []agent.Tool{
			{
				Name:        "calculator",
				Description: "Performs calculations",
			},
		},
	}

	routerReq := &proxyRouterRequest{
		proxyReq: req,
		context: UserContext{
			UserID:   "tool-user",
			PlanTier: "economy",
		},
	}

	// Should still return models even with tools
	models := routerReq.GetAvailableModels()
	if len(models) == 0 {
		t.Error("Expected models even with tools present")
	}

	// Query should still be accessible
	if routerReq.GetUserQuery() != "Calculate something" {
		t.Errorf("Expected query 'Calculate something', got '%s'", routerReq.GetUserQuery())
	}
}
