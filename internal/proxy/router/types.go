// Package router provides intelligent model selection using LLM-based routing.
package router

import "github.com/nevaria-ai/gateway/internal/proxy"

// Type aliases for proxy types
type (
	// UserContext is an alias for proxy.UserContext
	UserContext = proxy.UserContext
	// TokenEstimate is an alias for proxy.TokenEstimate
	TokenEstimate = proxy.TokenEstimate
	// CostEstimate is an alias for proxy.CostEstimate
	CostEstimate = proxy.CostEstimate
	// CostInfo is an alias for proxy.CostInfo
	CostInfo = proxy.CostInfo
)

// RouterRequest is sent to the router model
type RouterRequest struct {
	UserQuery        string              `json:"user_query"`
	AvailableModels  []string            `json:"available_models"`
	UserContext      UserContext         `json:"user_context"`
	ModelCosts       map[string]CostInfo `json:"model_costs"`
	PreviousAttempts []RouterAttempt     `json:"previous_attempts,omitempty"`
	UnhealthyModels  []string            `json:"unhealthy_models,omitempty"`
}

// RouterResponse is returned by the router model
type RouterResponse struct {
	SelectedModel   string        `json:"selected_model"`
	Reasoning       string        `json:"reasoning"`
	Confidence      string        `json:"confidence"`
	FallbackOptions []string      `json:"fallback_options"`
	EstimatedTokens TokenEstimate `json:"estimated_tokens"`
	EstimatedCost   CostEstimate  `json:"estimated_cost"`
}

// RouterAttempt represents a previous failed attempt
type RouterAttempt struct {
	Model string `json:"model"`
	Error string `json:"error"`
}
