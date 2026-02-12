// Package proxy provides LLM proxy functionality with routing, quota management, and usage tracking.
package proxy

import (
	"context"
	"fmt"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// LLMClient defines the interface for calling LLM APIs
type LLMClient interface {
	Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error)
}

// QuotaChecker defines the interface for checking user quota
type QuotaChecker interface {
	CheckQuota(ctx context.Context, userID string, estimatedCost float64) (*QuotaCheckResult, error)
	ReserveQuota(ctx context.Context, userID string, cost float64, source string) error
	DeductQuota(ctx context.Context, userID string, reserved, actual float64, source string) error
}

// QuotaCheckResult represents the result of a quota check
type QuotaCheckResult struct {
	HasQuota     bool
	Source       string
	PlanPercent  int
	PlanDollars  float64
	AddonDollars float64
	UsingAddon   bool
	UserContext  UserContext
}

// HealthChecker defines the interface for checking provider health
type HealthChecker interface {
	IsHealthy(ctx context.Context, provider, model string) (bool, error)
	RecordSuccess(ctx context.Context, provider, model string) error
	RecordFailure(ctx context.Context, provider, model string) error
}

// Router defines the interface for model selection
type Router interface {
	SelectModel(ctx context.Context, req RouterRequest) (*RouterResponse, error)
}

// RouterRequest is a simplified router request interface
type RouterRequest interface {
	GetUserQuery() string
	GetUserContext() UserContext
	GetAvailableModels() []string
}

// RouterResponse is a simplified router response interface
type RouterResponse struct {
	SelectedModel   string
	EstimatedTokens TokenEstimate
	EstimatedCost   CostEstimate
	FallbackOptions []string
}

// UsageTracker defines the interface for tracking usage
type UsageTracker interface {
	Track(ctx context.Context, event UsageEvent) error
}

// HandlerOptions configures a new Handler
type HandlerOptions struct {
	LLMClient LLMClient
	Quota     QuotaChecker
	Health    HealthChecker
	Router    Router
	Usage     UsageTracker
}

// Handler implements the main proxy request flow
type Handler struct {
	llmClient LLMClient
	quota     QuotaChecker
	health    HealthChecker
	router    Router
	usage     UsageTracker
}

// NewHandler creates a new proxy handler
func NewHandler(opts HandlerOptions) *Handler {
	return &Handler{
		llmClient: opts.LLMClient,
		quota:     opts.Quota,
		health:    opts.Health,
		router:    opts.Router,
		usage:     opts.Usage,
	}
}

// ProxyRequest represents a request to the proxy
type ProxyRequest struct {
	UserID    string
	SessionID string
	Query     string
	Tools     []agent.Tool
	Messages  []agent.AgentMessage
}

// ProxyResponse represents a response from the proxy
type ProxyResponse struct {
	Stream         <-chan agent.AgentEvent
	UsingAddon     bool
	AddonRemaining float64
}

// Errors
var (
	ErrQuotaExceeded  = &ProxyError{Op: "quota_check", Message: "quota exceeded"}
	ErrNoHealthyModel = &ProxyError{Op: "health_check", Message: "no healthy model available"}
	ErrRouterFailed   = &ProxyError{Op: "router", Message: "model selection failed"}
)

// ProxyError represents an error in proxy operations
type ProxyError struct {
	Op      string
	Message string
	Err     error
}

func (e *ProxyError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Message)
}

func (e *ProxyError) Unwrap() error {
	return e.Err
}

// Handle processes a proxy request through the full flow
func (h *Handler) Handle(ctx context.Context, req ProxyRequest) (*ProxyResponse, error) {
	// 1. QUOTA CHECK (if quota checker is configured)
	var quotaResult *QuotaCheckResult
	var estimatedCost float64

	if h.quota != nil {
		// Estimate cost for routing
		estimatedCost = 0.001 // Default estimate

		result, err := h.quota.CheckQuota(ctx, req.UserID, estimatedCost)
		if err != nil {
			return nil, fmt.Errorf("quota check failed: %w", err)
		}

		if !result.HasQuota {
			return nil, ErrQuotaExceeded
		}

		quotaResult = result
	}

	// 2. ROUTER (if router is configured)
	var routerResp *RouterResponse
	if h.router != nil {
		// For now, use a simple router request
		// TODO: Build proper router request from ProxyRequest
		resp, err := h.router.SelectModel(ctx, &simpleRouterRequest{
			query:   req.Query,
			context: quotaResult.UserContext,
		})
		if err != nil {
			return nil, fmt.Errorf("router failed: %w", err)
		}
		routerResp = resp
		estimatedCost = routerResp.EstimatedCost.Total
	}

	// 3. HEALTH CHECK (if health checker is configured)
	selectedModel := "gpt-4o-mini" // Default fallback
	if routerResp != nil {
		selectedModel = routerResp.SelectedModel
	}

	if h.health != nil {
		healthy, err := h.health.IsHealthy(ctx, "", selectedModel)
		if err != nil || !healthy {
			// Try fallback option
			if routerResp != nil && len(routerResp.FallbackOptions) > 0 {
				selectedModel = routerResp.FallbackOptions[0]
			}
		}
	}

	// 4. RESERVE QUOTA
	if h.quota != nil && quotaResult != nil {
		source := quotaResult.Source
		if source == "" {
			source = "plan"
		}
		if err := h.quota.ReserveQuota(ctx, req.UserID, estimatedCost, source); err != nil {
			return nil, fmt.Errorf("quota reserve failed: %w", err)
		}
	}

	// 5. LLM CALL
	eventCh, err := h.llmClient.Stream(ctx, req.Messages, req.Tools)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	// 6. Spawn goroutine to track usage after stream
	// TODO: Implement usage tracking with actual token counting
	// For now, just return the event channel directly

	addonRemaining := 0.0
	usingAddon := false
	if quotaResult != nil {
		addonRemaining = quotaResult.AddonDollars
		usingAddon = quotaResult.UsingAddon
	}

	return &ProxyResponse{
		Stream:         eventCh,
		UsingAddon:     usingAddon,
		AddonRemaining: addonRemaining,
	}, nil
}

// simpleRouterRequest is a simple implementation of RouterRequest
type simpleRouterRequest struct {
	query   string
	context UserContext
}

func (r *simpleRouterRequest) GetUserQuery() string {
	return r.query
}

func (r *simpleRouterRequest) GetUserContext() UserContext {
	return r.context
}

func (r *simpleRouterRequest) GetAvailableModels() []string {
	return []string{"gpt-4o-mini", "claude-3-5-haiku"}
}
