// Package proxy provides the proxy client that implements agent.LLMClient.
package proxy

import (
	"context"

	"github.com/nevaria-ai/gateway/internal/agent"
)

// Client implements agent.LLMClient with quota/routing/health checks.
// It wraps the proxy Handler and adds user context to each request.
type Client struct {
	handler *Handler
	userID  string
}

// NewClient creates a new proxy client for a specific user.
func NewClient(handler *Handler, userID string) *Client {
	return &Client{
		handler: handler,
		userID:  userID,
	}
}

// Stream sends messages through the proxy flow (quota → router → health → LLM → usage).
// It implements agent.LLMClient.Stream.
func (c *Client) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	// Extract text from last user message for query
	var query string
	if len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if len(lastMsg.Content) > 0 {
			if tc, ok := lastMsg.Content[0].(agent.TextContent); ok {
				query = tc.Text
			}
		}
	}

	// Build proxy request
	req := ProxyRequest{
		UserID:   c.userID,
		Query:    query,
		Tools:    tools,
		Messages: messages,
	}

	// Call proxy handler
	resp, err := c.handler.Handle(ctx, req)
	if err != nil {
		return nil, err
	}

	// Return event channel from handler
	return resp.Stream, nil
}
