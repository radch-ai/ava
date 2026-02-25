package websocket

import (
	"context"

	"github.com/nevaria-ai/gateway/internal/agent"
	llmproxy "github.com/nevaria-ai/llm-proxy-sdk"
)

// SDKClient implements agent.LLMClient using the SDK handler.
type SDKClient struct {
	handler *llmproxy.Handler
	userID  string
}

// NewSDKClient creates a new SDK client for a specific user.
func NewSDKClient(handler *llmproxy.Handler, userID string) *SDKClient {
	return &SDKClient{
		handler: handler,
		userID:  userID,
	}
}

// Stream sends messages through the SDK handler.
func (c *SDKClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	// Convert agent types to SDK types
	sdkMessages := agent.MessagesToSDK(messages)
	sdkTools := agent.ToolsToSDK(tools)

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

	// Convert SDK message pointers to values for SDK handler
	messagesSlice := make([]llmproxy.Message, len(sdkMessages))
	for i, m := range sdkMessages {
		if m != nil {
			messagesSlice[i] = *m
		}
	}

	// Build SDK proxy request
	req := llmproxy.ProxyRequest{
		UserID:    c.userID,
		SessionID: c.userID + ":session",
		Query:     query,
		Tools:     sdkTools,
		Messages:  messagesSlice,
	}

	// Call SDK handler
	resp, err := c.handler.Handle(ctx, req)
	if err != nil {
		return nil, err
	}

	// Wrap SDK events to agent events
	agentEventCh := make(chan agent.AgentEvent, 100)

	go func() {
		defer close(agentEventCh)

		for event := range resp.Stream {
			// Convert SDK event to agent event
			switch e := event.(type) {
			case llmproxy.MessageStartEvent:
				// Convert SDK message to agent message
				agentMsg := agent.MessageFromSDK(&e.Message)
				agentEventCh <- agent.MessageStartEvent{Timestamp: e.Timestamp, Message: agentMsg}
			case llmproxy.MessageUpdateEvent:
				// Convert SDK message to agent message
				agentMsg := agent.MessageFromSDK(&e.Message)
				agentEventCh <- agent.MessageUpdateEvent{Timestamp: e.Timestamp, Message: agentMsg, Delta: e.Delta}
			case llmproxy.MessageEndEvent:
				// Convert SDK message to agent message
				agentMsg := agent.MessageFromSDK(&e.Message)
				agentEventCh <- agent.MessageEndEvent{Timestamp: e.Timestamp, Message: agentMsg}
			case llmproxy.TokenUsageEvent:
				agentEventCh <- agent.TokenUsageEvent{Timestamp: e.Timestamp, TokensInput: int64(e.TokensInput), TokensOutput: int64(e.TokensOutput)}
			case llmproxy.DoneEvent:
				return
			case llmproxy.ErrorEvent:
				// For errors, we'll emit a tool execution error event to indicate failure
				// This is a simplification - in practice you might want different error handling
				toolErrEvent := agent.ToolExecutionEndEvent{
					Timestamp:  e.Timestamp,
					ToolCallID: "",
					ToolName:   "llm_proxy",
					Result:     e.Message,
					IsError:    true,
				}
				agentEventCh <- toolErrEvent
			}
		}
	}()

	return agentEventCh, nil
}
