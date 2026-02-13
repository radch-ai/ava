// Package proxy provides LLM proxy functionality with routing, quota management, and usage tracking.
package proxy

import (
	"context"
	"fmt"
	"strings"
	"time"

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

// Handler implements the main LLM proxy request flow
type Handler struct {
	llmClient LLMClient
	quota     QuotaChecker
	health    HealthChecker
	router    Router
	usage     UsageTracker
}

// NewHandler creates a new LLM proxy handler
func NewHandler(opts HandlerOptions) *Handler {
	return &Handler{
		llmClient: opts.LLMClient,
		quota:     opts.Quota,
		health:    opts.Health,
		router:    opts.Router,
		usage:     opts.Usage,
	}
}

// ProxyRequest represents a request to the LLM proxy
type ProxyRequest struct {
	UserID    string
	SessionID string
	Query     string
	Tools     []agent.Tool
	Messages  []agent.AgentMessage
}

// ProxyResponse represents a response from the LLM proxy
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

// ProxyError represents an error in LLM proxy operations
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

// Handle processes an LLM proxy request through the full flow
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
		// Build proper router request from ProxyRequest with full context
		routerReq := &proxyRouterRequest{
			proxyReq: req,
			context:  quotaResult.UserContext,
		}
		resp, err := h.router.SelectModel(ctx, routerReq)
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

	// 6. Wrap event channel to track usage
	trackedEventCh := h.trackUsage(ctx, eventCh, req.UserID, req.SessionID, selectedModel, estimatedCost, quotaResult)

	addonRemaining := 0.0
	usingAddon := false
	if quotaResult != nil {
		addonRemaining = quotaResult.AddonDollars
		usingAddon = quotaResult.UsingAddon
	}

	return &ProxyResponse{
		Stream:         trackedEventCh,
		UsingAddon:     usingAddon,
		AddonRemaining: addonRemaining,
	}, nil
}

// trackUsage wraps the event channel to track token usage and emit usage events.
func (h *Handler) trackUsage(ctx context.Context, eventCh <-chan agent.AgentEvent, userID, sessionID, model string, estimatedCost float64, quotaResult *QuotaCheckResult) <-chan agent.AgentEvent {
	// Determine provider from model name
	provider := "unknown"
	switch {
	case model == "gpt-4":
		fallthrough
	case model == "gpt-4o":
		fallthrough
	case strings.HasPrefix(model, "gpt-"):
		provider = "openai"
	case strings.HasPrefix(model, "claude-"):
		provider = "anthropic"
	case strings.HasPrefix(model, "gemini-"):
		provider = "google"
	}

	trackedCh := make(chan agent.AgentEvent, 100)

	go func() {
		defer close(trackedCh)

		var tokensInput int64
		var tokensOutput int64
		usageTracked := false

		for event := range eventCh {
			// Forward all events
			trackedCh <- event

			// Track token usage
			switch e := event.(type) {
			case agent.TokenUsageEvent:
				tokensInput += e.TokensInput
				tokensOutput += e.TokensOutput
			case agent.MessageEndEvent:
				// Create usage event when message ends
				if !usageTracked && h.usage != nil {
					// Calculate actual cost from token counts
					// For now, use the estimated cost if we don't have pricing info
					actualCost := estimatedCost

					// Determine charged source
					chargedFrom := "plan"
					if quotaResult != nil && quotaResult.UsingAddon {
						chargedFrom = "addon"
					}

					usageEvent := UsageEvent{
						UserID:       userID,
						SessionID:    sessionID,
						Model:        model,
						Provider:     provider,
						TokensInput:  int(tokensInput),
						TokensOutput: int(tokensOutput),
						CostTotal:    actualCost,
						ChargedFrom:  chargedFrom,
						Timestamp:    e.Timestamp,
					}

					// Track usage asynchronously (don't block the stream)
					go func() {
						_ = h.usage.Track(context.Background(), usageEvent)
					}()

					usageTracked = true
				}
			}
		}

		// Handle case where stream ended without MessageEndEvent
		// but we have token usage information
		if !usageTracked && (tokensInput > 0 || tokensOutput > 0) && h.usage != nil {
			actualCost := estimatedCost
			chargedFrom := "plan"
			if quotaResult != nil && quotaResult.UsingAddon {
				chargedFrom = "addon"
			}

			usageEvent := UsageEvent{
				UserID:       userID,
				SessionID:    sessionID,
				Model:        model,
				Provider:     provider,
				TokensInput:  int(tokensInput),
				TokensOutput: int(tokensOutput),
				CostTotal:    actualCost,
				ChargedFrom:  chargedFrom,
				Timestamp:    time.Now().Unix(),
			}

			_ = h.usage.Track(context.Background(), usageEvent)
		}
	}()

	return trackedCh
}

// proxyRouterRequest implements RouterRequest using full ProxyRequest context
type proxyRouterRequest struct {
	proxyReq ProxyRequest
	context  UserContext
}

// GetUserQuery returns the user's query text
func (r *proxyRouterRequest) GetUserQuery() string {
	return r.proxyReq.Query
}

// GetUserContext returns the user's quota and plan context
func (r *proxyRouterRequest) GetUserContext() UserContext {
	return r.context
}

// GetAvailableModels returns the list of available models based on tools and context
// This can be enhanced to filter models based on tool compatibility, user tier, etc.
func (r *proxyRouterRequest) GetAvailableModels() []string {
	// Default available models
	models := []string{"gpt-4o-mini", "claude-3-5-haiku", "gpt-4o", "claude-3-5-sonnet"}

	// Add more capable models if user has premium tier
	if r.context.PlanTier == "premium" || r.context.PlanTier == "enterprise" {
		models = append(models, "claude-3-opus", "gpt-4-turbo")
	}

	// Filter models based on tool requirements if tools are present
	// Some models may not support all tools
	// For now, return all models - this can be enhanced with tool compatibility checks
	return models
}
