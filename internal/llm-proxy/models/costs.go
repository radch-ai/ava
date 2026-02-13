package models

import (
	"github.com/nevaria-ai/gateway/internal/llm-proxy"
)

// CalculateCost computes the cost for a given model and token usage
func CalculateCost(cfg *ModelConfig, inputTokens, outputTokens int) float64 {
	inputCost := float64(inputTokens) * cfg.InputCostPerMillion / 1000000
	outputCost := float64(outputTokens) * cfg.OutputCostPerMillion / 1000000
	return inputCost + outputCost
}

// EstimateCost calculates estimated cost from token estimates
func EstimateCost(cfg *ModelConfig, estimate *proxy.TokenEstimate) *proxy.CostEstimate {
	inputCost := float64(estimate.Input) * cfg.InputCostPerMillion / 1000000
	outputCost := float64(estimate.Output) * cfg.OutputCostPerMillion / 1000000

	return &proxy.CostEstimate{
		Input:  inputCost,
		Output: outputCost,
		Total:  inputCost + outputCost,
	}
}
