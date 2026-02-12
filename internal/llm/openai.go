// Package llm provides OpenAI LLM client implementation.
package llm

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/shared"
)

// OpenAIClientConfig holds configuration for the OpenAI client.
type OpenAIClientConfig struct {
	Client openai.Client
	Model  agent.Model
}

// OpenAIClient is the OpenAI provider implementation.
type OpenAIClient struct {
	config OpenAIClientConfig
}

// NewOpenAIClient creates a new OpenAI client.
func NewOpenAIClient(config OpenAIClientConfig) *OpenAIClient {
	return &OpenAIClient{
		config: config,
	}
}

// Stream sends messages to the OpenAI API and returns a channel of events.
func (c *OpenAIClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	// Convert messages to OpenAI format
	openaiMessages := c.convertMessages(messages)

	// Convert tools to OpenAI format
	var openaiTools []openai.ChatCompletionToolParam
	if len(tools) > 0 {
		openaiTools = c.convertTools(tools)
	}

	// Build request parameters
	params := openai.ChatCompletionNewParams{
		Messages: openaiMessages,
		Model:    openai.ChatModel(c.config.Model.ID),
	}

	// Add tools if present
	if len(openaiTools) > 0 {
		params.Tools = openaiTools
	}

	// Call streaming API
	stream := c.config.Client.Chat.Completions.NewStreaming(ctx, params)

	// Check for immediate errors (e.g., connection refused, 4xx status codes)
	if err := stream.Err(); err != nil {
		return nil, err
	}

	// Parse stream
	return c.parseOpenAIStream(ctx, stream)
}

// convertMessages converts agent messages to OpenAI format.
func (c *OpenAIClient) convertMessages(messages []agent.AgentMessage) []openai.ChatCompletionMessageParamUnion {
	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))

	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleUser:
			// User message
			content := getContentText(msg)
			userMsg := openai.UserMessage(content)
			result = append(result, userMsg)

		case agent.RoleAssistant:
			// Assistant message - may have tool calls
			content := getContentText(msg)
			assistantMsg := openai.AssistantMessage(content)

			// Check for tool calls in content
			toolCalls := c.extractToolCalls(msg)
			if len(toolCalls) > 0 {
				// Add tool calls to the message
				assistantMsgStruct := openai.ChatCompletionAssistantMessageParam{
					ToolCalls: toolCalls,
				}
				if content != "" {
					assistantMsgStruct.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: openai.String(content),
					}
				}
				result = append(result, openai.ChatCompletionMessageParamUnion{
					OfAssistant: &assistantMsgStruct,
				})
			} else {
				result = append(result, assistantMsg)
			}

		case agent.RoleToolResult:
			// Tool result message
			content := getContentText(msg)
			toolMsg := openai.ToolMessage(content, msg.ToolCallID)
			result = append(result, toolMsg)
		}
	}

	return result
}

// convertTools converts agent tools to OpenAI format.
func (c *OpenAIClient) convertTools(tools []agent.Tool) []openai.ChatCompletionToolParam {
	convertedTools := ToOpenAITools(tools)
	result := make([]openai.ChatCompletionToolParam, len(convertedTools))

	for i, tool := range convertedTools {
		var functionParams shared.FunctionDefinitionParam

		functionParams.Name = tool.Function.Name
		if tool.Function.Description != "" {
			functionParams.Description = openai.String(tool.Function.Description)
		}

		// Convert parameters from JSONSchema to map[string]any
		if tool.Function.Parameters != nil {
			// Convert JSONSchema to params format
			paramsJSON, _ := json.Marshal(tool.Function.Parameters)
			var paramsMap map[string]any
			if err := json.Unmarshal(paramsJSON, &paramsMap); err == nil {
				functionParams.Parameters = shared.FunctionParameters(paramsMap)
			}
		}

		result[i] = openai.ChatCompletionToolParam{
			Function: functionParams,
		}
	}

	return result
}

// extractToolCalls extracts tool calls from an assistant message.
func (c *OpenAIClient) extractToolCalls(msg agent.AgentMessage) []openai.ChatCompletionMessageToolCallParam {
	var toolCalls []openai.ChatCompletionMessageToolCallParam

	for _, block := range msg.Content {
		if toolCall, ok := block.(agent.ToolCallContent); ok {
			// Build arguments string
			argsJSON, _ := json.Marshal(toolCall.Arguments)

			function := openai.ChatCompletionMessageToolCallFunctionParam{
				Name:      toolCall.Name,
				Arguments: string(argsJSON),
			}

			toolCallParam := openai.ChatCompletionMessageToolCallParam{
				ID:       toolCall.ID,
				Function: function,
			}

			toolCalls = append(toolCalls, toolCallParam)
		}
	}

	return toolCalls
}

// parseOpenAIStream parses OpenAI streaming response and emits events.
func (c *OpenAIClient) parseOpenAIStream(ctx context.Context, stream *ssestream.Stream[openai.ChatCompletionChunk]) (<-chan agent.AgentEvent, error) {
	eventCh := make(chan agent.AgentEvent, 100)

	go func() {
		defer close(eventCh)
		defer func() { _ = stream.Close() }()

		var currentMessage *agent.AgentMessage
		var currentText strings.Builder
		var currentBlocks []agent.ContentBlock
		var toolCallBuffers map[int64]*toolCallBuffer
		timestamp := time.Now().Unix()

		for stream.Next() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			chunk := stream.Current()

			// Initialize message on first chunk
			if currentMessage == nil {
				currentMessage = &agent.AgentMessage{
					Role:    agent.RoleAssistant,
					Content: []agent.ContentBlock{},
				}
				eventCh <- agent.MessageStartEvent{
					Timestamp: timestamp,
					Message:   *currentMessage,
				}
			}

			// Emit token usage event if available
			if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
				eventCh <- agent.TokenUsageEvent{
					Timestamp:    timestamp,
					TokensInput:  chunk.Usage.PromptTokens,
					TokensOutput: chunk.Usage.CompletionTokens,
				}
			}

			// Process choices (we only use index 0)
			if len(chunk.Choices) > 0 {
				choice := chunk.Choices[0]
				delta := choice.Delta

				// Handle text content
				if delta.Content != "" {
					currentText.WriteString(delta.Content)
					eventCh <- agent.MessageUpdateEvent{
						Timestamp: timestamp,
						Message: agent.AgentMessage{
							Role:    agent.RoleAssistant,
							Content: []agent.ContentBlock{agent.TextContent{Text: currentText.String()}},
						},
						Delta: delta.Content,
					}
				}

				// Handle tool calls
				if len(delta.ToolCalls) > 0 {
					if toolCallBuffers == nil {
						toolCallBuffers = make(map[int64]*toolCallBuffer)
					}

					for _, tc := range delta.ToolCalls {
						buf := toolCallBuffers[tc.Index]
						if buf == nil {
							buf = &toolCallBuffer{
								Index: tc.Index,
							}
							toolCallBuffers[tc.Index] = buf
						}

						// Set ID if provided
						if tc.ID != "" {
							buf.ID = tc.ID
						}

						// Set name if provided
						if tc.Function.Name != "" {
							buf.Name = tc.Function.Name
						}

						// Append arguments
						if tc.Function.Arguments != "" {
							buf.ArgsJSON.WriteString(tc.Function.Arguments)
						}
					}
				}

				// Check if we're done
				if choice.FinishReason != "" {
					// Flush accumulated text
					if currentText.Len() > 0 {
						currentBlocks = append(currentBlocks, agent.TextContent{Text: currentText.String()})
						currentText.Reset()
					}

					// Parse and add tool calls
					for _, buf := range toolCallBuffers {
						var args map[string]any
						if buf.ArgsJSON.Len() > 0 {
							if err := json.Unmarshal([]byte(buf.ArgsJSON.String()), &args); err != nil {
								args = make(map[string]any)
							}
						}

						currentBlocks = append(currentBlocks, agent.ToolCallContent{
							ID:        buf.ID,
							Name:      buf.Name,
							Arguments: args,
						})
					}

					// Update message with final content
					currentMessage.Content = currentBlocks
					currentMessage.Timestamp = timestamp

					eventCh <- agent.MessageEndEvent{
						Timestamp: timestamp,
						Message:   *currentMessage,
					}

					return
				}
			}
		}

		// Check for stream error
		if err := stream.Err(); err != nil {
			return
		}
	}()

	return eventCh, nil
}

// toolCallBuffer accumulates tool call data across streaming chunks.
type toolCallBuffer struct {
	Index    int64
	ID       string
	Name     string
	ArgsJSON strings.Builder
}

// getContentText extracts text content from a message.
func getContentText(msg agent.AgentMessage) string {
	for _, block := range msg.Content {
		if tc, ok := block.(agent.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
