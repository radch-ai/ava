// Package llm provides Anthropic LLM client implementation using the official SDK.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
	"github.com/nevaria-ai/gateway/internal/agent"
)

// AnthropicClientConfig holds configuration for the Anthropic client.
type AnthropicClientConfig struct {
	APIKey        string
	BaseURL       string
	Model         string
	Timeout       time.Duration
	ThinkingLevel agent.ThinkingLevel
}

// AnthropicClient is the Anthropic provider implementation using the official SDK.
type AnthropicClient struct {
	config AnthropicClientConfig
	client anthropic.Client
	model  agent.Model
}

// NewAnthropicClient creates a new Anthropic client using the official SDK.
func NewAnthropicClient(config AnthropicClientConfig) *AnthropicClient {
	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}

	// Build SDK options
	opts := []option.RequestOption{
		option.WithAPIKey(config.APIKey),
		option.WithRequestTimeout(time.Duration(config.Timeout)),
	}

	// Set custom base URL if provided
	if config.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(config.BaseURL))
	}

	// Create SDK client
	client := anthropic.NewClient(opts...)

	return &AnthropicClient{
		config: config,
		client: client,
		model:  agent.Model{ID: config.Model, Provider: "anthropic"},
	}
}

// Stream sends messages to the Anthropic API and returns a channel of events.
func (c *AnthropicClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	// Convert messages to SDK format
	sdkMessages := c.convertMessagesToSDK(messages)

	// Convert tools to SDK format
	var sdkTools []anthropic.ToolUnionParam
	if len(tools) > 0 {
		sdkTools = c.convertToolsToSDK(tools)
	}

	// Build request parameters
	params := anthropic.MessageNewParams{
		MaxTokens: int64(4096),
		Messages:  sdkMessages,
		Tools:     sdkTools,
		Model:     anthropic.Model(c.config.Model),
	}

	// Add thinking parameter if configured
	thinking := c.convertThinkingLevelToSDK()
	if thinking != nil {
		params.Thinking = *thinking
	}

	// Call streaming API
	stream := c.client.Messages.NewStreaming(ctx, params)

	// Check for immediate errors
	if err := stream.Err(); err != nil {
		return nil, fmt.Errorf("failed to start stream: %w", err)
	}

	// Parse stream
	return c.parseSDKStream(ctx, stream)
}

// convertMessagesToSDK converts agent messages to Anthropic SDK message format.
func (c *AnthropicClient) convertMessagesToSDK(messages []agent.AgentMessage) []anthropic.MessageParam {
	result := make([]anthropic.MessageParam, 0, len(messages))

	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleUser:
			// User message - can contain text, images, or tool results
			content := c.convertContentBlocks(msg.Content)

			// Check if this is a tool result message
			if msg.ToolCallID != "" {
				// Tool result message - use helper
				contentText := ""
				if len(msg.Content) > 0 {
					if tc, ok := msg.Content[0].(agent.TextContent); ok {
						contentText = tc.Text
					}
				}
				toolResult := anthropic.NewToolResultBlock(msg.ToolCallID, contentText, msg.IsError)

				userMsg := anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleUser,
					Content: []anthropic.ContentBlockParamUnion{toolResult},
				}
				result = append(result, userMsg)
			} else {
				// Regular user message
				userMsg := anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleUser,
					Content: content,
				}
				result = append(result, userMsg)
			}

		case agent.RoleAssistant:
			// Assistant message - may have tool calls
			content := c.convertContentBlocks(msg.Content)
			assistantMsg := anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleAssistant,
				Content: content,
			}
			result = append(result, assistantMsg)

		case agent.RoleToolResult:
			// Tool results are sent as user messages in Anthropic's API
			contentText := ""
			if len(msg.Content) > 0 {
				if tc, ok := msg.Content[0].(agent.TextContent); ok {
					contentText = tc.Text
				}
			}
			toolResult := anthropic.NewToolResultBlock(msg.ToolCallID, contentText, msg.IsError)

			userMsg := anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{toolResult},
			}
			result = append(result, userMsg)
		}
	}

	return result
}

// convertContentBlocks converts agent content blocks to Anthropic SDK content format.
func (c *AnthropicClient) convertContentBlocks(blocks []agent.ContentBlock) []anthropic.ContentBlockParamUnion {
	result := make([]anthropic.ContentBlockParamUnion, 0, len(blocks))

	for _, block := range blocks {
		switch b := block.(type) {
		case agent.TextContent:
			textBlock := &anthropic.TextBlockParam{
				Text: b.Text,
			}
			result = append(result, anthropic.ContentBlockParamUnion{OfText: textBlock})

		case agent.ImageContent:
			source := anthropic.Base64ImageSourceParam{
				Data: b.Data,
			}
			if b.MimeType != "" {
				source.MediaType = anthropic.Base64ImageSourceMediaType(b.MimeType)
			}
			imageSourceUnion := anthropic.ImageBlockParamSourceUnion{
				OfBase64: &source,
			}
			imageBlock := &anthropic.ImageBlockParam{
				Source: imageSourceUnion,
			}
			result = append(result, anthropic.ContentBlockParamUnion{OfImage: imageBlock})

		case agent.ToolCallContent:
			toolUseBlock := &anthropic.ToolUseBlockParam{
				ID:    b.ID,
				Name:  b.Name,
				Input: b.Arguments,
			}
			result = append(result, anthropic.ContentBlockParamUnion{OfToolUse: toolUseBlock})
		}
	}

	return result
}

// convertToolsToSDK converts agent tools to Anthropic SDK tool format.
func (c *AnthropicClient) convertToolsToSDK(tools []agent.Tool) []anthropic.ToolUnionParam {
	anthropicTools := ToAnthropicTools(tools)
	result := make([]anthropic.ToolUnionParam, len(anthropicTools))

	for i, tool := range anthropicTools {
		// Convert input schema to map[string]any for the SDK
		inputSchema := make(map[string]any)
		if tool.InputSchema != nil {
			inputSchema["type"] = tool.InputSchema.Type
			if len(tool.InputSchema.Properties) > 0 {
				inputSchema["properties"] = tool.InputSchema.Properties
			}
			if len(tool.InputSchema.Required) > 0 {
				inputSchema["required"] = tool.InputSchema.Required
			}
		}

		// Marshal to JSON and let SDK unmarshal it properly
		schemaJSON, _ := json.Marshal(inputSchema)
		var toolInputSchema anthropic.ToolInputSchemaParam
		_ = json.Unmarshal(schemaJSON, &toolInputSchema)

		toolParam := &anthropic.ToolParam{
			Name:        tool.Name,
			Description: anthropic.String(tool.Description),
			InputSchema: toolInputSchema,
		}

		result[i] = anthropic.ToolUnionParam{OfTool: toolParam}
	}

	return result
}

// convertThinkingLevelToSDK converts thinking level to SDK format.
func (c *AnthropicClient) convertThinkingLevelToSDK() *anthropic.ThinkingConfigParamUnion {
	if c.config.ThinkingLevel.IsOff() {
		return nil
	}

	budget := int64(0)
	switch c.config.ThinkingLevel {
	case agent.ThinkingLevelMinimal:
		budget = 1000
	case agent.ThinkingLevelLow:
		budget = 3000
	case agent.ThinkingLevelMedium:
		budget = 12000
	case agent.ThinkingLevelHigh:
		budget = 24000
	}

	thinking := &anthropic.ThinkingConfigEnabledParam{
		BudgetTokens: budget,
	}

	return &anthropic.ThinkingConfigParamUnion{OfEnabled: thinking}
}

// parseSDKStream parses Anthropic SDK streaming response and emits events.
func (c *AnthropicClient) parseSDKStream(ctx context.Context, stream *ssestream.Stream[anthropic.MessageStreamEventUnion]) (<-chan agent.AgentEvent, error) {
	eventCh := make(chan agent.AgentEvent, 100)

	go func() {
		defer close(eventCh)
		_ = stream.Close()

		var currentMessage *agent.AgentMessage
		var currentText strings.Builder
		var currentBlocks []agent.ContentBlock
		var buf *anthropicToolCallBuffer
		timestamp := time.Now().Unix()

		for stream.Next() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			event := stream.Current()

			// Use Type field which is a string: "message_start", "content_block_start", etc.
			switch event.Type {
			case "message_start":
				currentMessage = &agent.AgentMessage{
					Role:    agent.RoleAssistant,
					Content: []agent.ContentBlock{},
				}
				eventCh <- agent.MessageStartEvent{
					Timestamp: timestamp,
					Message:   *currentMessage,
				}

			case "content_block_start":
				contentBlock := event.ContentBlock
				// Check type - AsToolUse will return zero value if not tool use
				toolUseBlock := contentBlock.AsToolUse()

				// If it's a tool use (non-empty ID), initialize buffer
				if toolUseBlock.ID != "" {
					newBuf := anthropicToolCallBuffer{
						ID:        toolUseBlock.ID,
						Name:      toolUseBlock.Name,
						Arguments: make(map[string]any),
					}

					// Parse input JSON if provided
					if len(toolUseBlock.Input) > 0 {
						var args map[string]any
						if err := json.Unmarshal(toolUseBlock.Input, &args); err == nil {
							newBuf.Arguments = args
						}
					}
					buf = &newBuf
				} else {
					// It's a text block, reset builder
					currentText.Reset()
				}

			case "content_block_delta":
				delta := event.Delta
				// Delta.Type will be "text_delta" for text content
				if delta.Type == "text_delta" && delta.Text != "" {
					currentText.WriteString(delta.Text)
					eventCh <- agent.MessageUpdateEvent{
						Timestamp: timestamp,
						Message: agent.AgentMessage{
							Role:    agent.RoleAssistant,
							Content: []agent.ContentBlock{agent.TextContent{Text: currentText.String()}},
						},
						Delta: delta.Text,
					}
				}

			case "content_block_stop":
				// Flush accumulated text
				if currentText.Len() > 0 {
					currentBlocks = append(currentBlocks, agent.TextContent{Text: currentText.String()})
					currentText.Reset()
				}

				// Flush tool use buffer
				if buf != nil {
					currentBlocks = append(currentBlocks, agent.ToolCallContent{
						ID:        buf.ID,
						Name:      buf.Name,
						Arguments: buf.Arguments,
					})
					buf = nil
				}

			case "message_delta":
				if currentMessage != nil && len(currentBlocks) > 0 {
					currentMessage.Content = currentBlocks
					currentMessage.Timestamp = timestamp
				}

			case "message_stop":
				if currentMessage != nil {
					if len(currentMessage.Content) == 0 && len(currentBlocks) > 0 {
						currentMessage.Content = currentBlocks
					}
					currentMessage.Timestamp = timestamp
				}
				eventCh <- agent.MessageEndEvent{
					Timestamp: timestamp,
					Message:   *currentMessage,
				}
			}
		}

		// Check for stream error
		if err := stream.Err(); err != nil {
			// Log error but don't send it through channel
			// The channel is already closed by defer
			return
		}
	}()

	return eventCh, nil
}

// anthropicToolCallBuffer accumulates tool call data during streaming.
type anthropicToolCallBuffer struct {
	ID        string
	Name      string
	Arguments map[string]any
}
