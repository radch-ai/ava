// Package router provides intelligent model selection using LLM-based routing.
package router

import (
	"fmt"
	"strings"
)

// BuildRouterPrompt creates a structured prompt for the router model
func BuildRouterPrompt(req RouterRequest) string {
	var sb strings.Builder

	sb.WriteString("You are an intelligent LLM model router. Your task is to select the most appropriate model ")
	sb.WriteString("based on the user's query, their plan tier, available models, and costs.\n\n")

	// User Query
	sb.WriteString("## User Query\n")
	sb.WriteString(req.UserQuery)
	sb.WriteString("\n\n")

	// User Context
	sb.WriteString("## User Context\n")
	fmt.Fprintf(&sb, "- Plan Tier: %s\n", req.UserContext.PlanTier)
	fmt.Fprintf(&sb, "- Quota Remaining: %d%%\n", req.UserContext.QuotaRemainingPercent)
	fmt.Fprintf(&sb, "- Plan Quota: $%.4f\n", req.UserContext.QuotaRemaining)
	if req.UserContext.HasAddonCredits {
		fmt.Fprintf(&sb, "- Addon Credits: $%.4f\n", req.UserContext.AddonRemaining)
	}
	fmt.Fprintf(&sb, "- Days Until Reset: %d\n", req.UserContext.DaysUntilReset)
	sb.WriteString("\n")

	// Available Models
	sb.WriteString("## Available Models\n")
	for _, model := range req.AvailableModels {
		if cost, ok := req.ModelCosts[model]; ok {
			fmt.Fprintf(&sb, "- %s: $%.4f/M input, $%.4f/M output\n",
				model, cost.InputCostPerMillion, cost.OutputCostPerMillion)
		} else {
			fmt.Fprintf(&sb, "- %s: (cost info unavailable)\n", model)
		}
	}
	sb.WriteString("\n")

	// Previous Attempts (if any)
	if len(req.PreviousAttempts) > 0 {
		sb.WriteString("## Previous Failed Attempts\n")
		for i, attempt := range req.PreviousAttempts {
			fmt.Fprintf(&sb, "%d. %s: %s\n", i+1, attempt.Model, attempt.Error)
		}
		sb.WriteString("\n")
	}

	// Unhealthy Models (if any)
	if len(req.UnhealthyModels) > 0 {
		sb.WriteString("## Unhealthy Models\n")
		sb.WriteString("The following models are currently unhealthy and should be avoided:\n")
		for _, model := range req.UnhealthyModels {
			fmt.Fprintf(&sb, "- %s\n", model)
		}
		sb.WriteString("\n")
	}

	// Instructions
	sb.WriteString("## Instructions\n")
	sb.WriteString("Select the best model for this request. Consider:\n")
	sb.WriteString("1. Query complexity (simple queries can use economy models)\n")
	sb.WriteString("2. User's plan tier and remaining quota\n")
	sb.WriteString("3. Cost efficiency\n")
	sb.WriteString("4. Model health status\n")
	sb.WriteString("5. Previous failures (if any)\n\n")

	sb.WriteString("Respond with JSON in this exact format:\n")
	sb.WriteString(`{
  "selected_model": "model-name",
  "reasoning": "brief explanation",
  "confidence": "high|medium|low",
  "fallback_options": ["model1", "model2"],
  "estimated_tokens": {"input": 100, "output": 50},
  "estimated_cost": {"input": 0.0001, "output": 0.0002, "total": 0.0003}
}`)

	return sb.String()
}
