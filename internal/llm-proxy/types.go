package proxy

import "time"

// UsageEvent records a single LLM usage event
type UsageEvent struct {
	UserID       string  `json:"user_id"`
	SessionID    string  `json:"session_id"`
	Model        string  `json:"model"`
	Provider     string  `json:"provider"`
	TokensInput  int     `json:"tokens_input"`
	TokensOutput int     `json:"tokens_output"`
	CostTotal    float64 `json:"cost_total"`
	ChargedFrom  string  `json:"charged_from"` // "plan" or "addon"
	Timestamp    int64   `json:"timestamp"`
}

// UserContext holds quota and plan information for routing
type UserContext struct {
	UserID                string  `json:"user_id"`
	PlanTier              string  `json:"plan_tier"`
	QuotaRemaining        float64 `json:"quota_remaining"`
	QuotaRemainingPercent int     `json:"quota_remaining_percent"`
	HasAddonCredits       bool    `json:"has_addon_credits"`
	AddonRemaining        float64 `json:"addon_remaining"`
	DaysUntilReset        int     `json:"days_until_reset"`
	IsUsingAddon          bool    `json:"is_using_addon"`
}

// ProviderHealth tracks the health status of a provider:model
type ProviderHealth struct {
	Status              string    `json:"status"` // healthy, degraded, unhealthy
	FailureCount        int       `json:"failure_count"`
	LastFailure         time.Time `json:"last_failure"`
	LastSuccess         time.Time `json:"last_success"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	CircuitOpenUntil    time.Time `json:"circuit_open_until,omitempty"`
}

// GatewayStatus represents the overall service status
type GatewayStatus string

const (
	StatusHealthy  GatewayStatus = "healthy"
	StatusDegraded GatewayStatus = "degraded"
	StatusCritical GatewayStatus = "critical"
	StatusPaused   GatewayStatus = "paused"
)

// TokenEstimate represents estimated token usage
type TokenEstimate struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

// CostEstimate represents estimated cost for a request
type CostEstimate struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
	Total  float64 `json:"total"`
}

// CostInfo holds pricing information for a model
type CostInfo struct {
	InputCostPerMillion  float64 `json:"input_cost_per_million"`
	OutputCostPerMillion float64 `json:"output_cost_per_million"`
}
