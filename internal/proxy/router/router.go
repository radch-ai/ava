// Package router provides intelligent model selection using LLM-based routing.
package router

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// LLMClient defines the interface for calling LLM APIs
type LLMClient interface {
	Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error)
}

// SelectModel calls the router model to select the best LLM
func SelectModel(ctx context.Context, llmClient LLMClient, req RouterRequest) (*RouterResponse, error) {
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client is required")
	}

	// Build router prompt from req
	prompt := BuildRouterPrompt(req)

	// Call router model (cheap, fast)
	messages := []agent.AgentMessage{
		agent.NewTextMessage(agent.RoleUser, prompt),
	}

	// Get response from LLM
	eventCh, err := llmClient.Stream(ctx, messages, []agent.Tool{})
	if err != nil {
		return nil, fmt.Errorf("router LLM call failed: %w", err)
	}

	// Collect the final message
	var assistantMsg *agent.AgentMessage
	for event := range eventCh {
		switch e := event.(type) {
		case agent.MessageEndEvent:
			assistantMsg = &e.Message
		case agent.TurnStartEvent, agent.TurnEndEvent, agent.MessageStartEvent, agent.MessageUpdateEvent:
			// Ignore other events
		}
	}

	if assistantMsg == nil {
		return nil, fmt.Errorf("no response from router model")
	}

	// Extract JSON from response
	var jsonText string
	for _, block := range assistantMsg.Content {
		if tc, ok := block.(agent.TextContent); ok {
			jsonText = tc.Text
			break
		}
	}

	if jsonText == "" {
		return nil, fmt.Errorf("router model returned empty response")
	}

	// Parse JSON response
	var resp RouterResponse
	if err := json.Unmarshal([]byte(jsonText), &resp); err != nil {
		return nil, fmt.Errorf("failed to parse router response: %w", err)
	}

	return &resp, nil
}
