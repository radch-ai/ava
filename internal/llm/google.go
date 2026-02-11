// Package llm provides Google GenAI client implementation.
package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"google.golang.org/genai"
)

// GoogleClientConfig holds configuration for the Google client.
type GoogleClientConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

// GoogleClient is the Google GenAI provider implementation.
type GoogleClient struct {
	config GoogleClientConfig
	client *genai.Client
	model  agent.Model
}

// NewGoogleClient creates a new Google client.
func NewGoogleClient(config GoogleClientConfig) *GoogleClient {
	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}

	return &GoogleClient{
		config: config,
		model:  agent.Model{ID: config.Model, Provider: "google"},
	}
}

// initClient initializes the GenAI client (lazy initialization).
func (c *GoogleClient) initClient(ctx context.Context) error {
	if c.client != nil {
		return nil
	}

	clientConfig := &genai.ClientConfig{
		APIKey: c.config.APIKey,
	}

	// Set custom base URL if provided
	if c.config.BaseURL != "" {
		clientConfig.HTTPOptions = genai.HTTPOptions{
			BaseURL: c.config.BaseURL,
		}
	}

	client, err := genai.NewClient(ctx, clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create Google GenAI client: %w", err)
	}

	c.client = client
	return nil
}

// Stream sends messages to the Google GenAI API and returns a channel of events.
func (c *GoogleClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	if err := c.initClient(ctx); err != nil {
		return nil, err
	}

	// Convert agent messages to genai.Content format
	contents := c.convertMessagesToGenai(messages)

	// Convert tools to genai format
	var genaiTools []*genai.Tool
	if len(tools) > 0 {
		genaiTools = c.convertToolsToGenai(tools)
	}

	eventCh := make(chan agent.AgentEvent, 100)

	go func() {
		defer close(eventCh)
		timestamp := time.Now().Unix()

		// Build the generate config
		config := &genai.GenerateContentConfig{}
		if len(genaiTools) > 0 {
			config.Tools = genaiTools
		}

		// Call the streaming API
		streamIter := c.client.Models.GenerateContentStream(ctx, c.config.Model, contents, config)

		var currentMessage *agent.AgentMessage
		var currentText strings.Builder
		var currentBlocks []agent.ContentBlock
		var functionCallBuffers []*functionCallBuffer
		sentMessageStart := false

		for result, err := range streamIter {
			if err != nil {
				// On error, close channel without sending any more events
				// The error is propagated through the iterator
				return
			}

			// Process the result
			if len(result.Candidates) == 0 {
				continue
			}

			candidate := result.Candidates[0]

			// Send message start on first chunk
			if !sentMessageStart {
				currentMessage = &agent.AgentMessage{
					Role:    agent.RoleAssistant,
					Content: []agent.ContentBlock{},
				}
				eventCh <- agent.MessageStartEvent{
					Timestamp: timestamp,
					Message:   *currentMessage,
				}
				sentMessageStart = true
			}

			// Process content parts
			if candidate.Content != nil && len(candidate.Content.Parts) > 0 {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						currentText.WriteString(part.Text)
						eventCh <- agent.MessageUpdateEvent{
							Timestamp: timestamp,
							Message: agent.AgentMessage{
								Role:    agent.RoleAssistant,
								Content: []agent.ContentBlock{agent.TextContent{Text: currentText.String()}},
							},
							Delta: part.Text,
						}
					}
					if part.FunctionCall != nil {
						// Accumulate function call data
						functionCallBuffers = append(functionCallBuffers, &functionCallBuffer{
							name: part.FunctionCall.Name,
							args: part.FunctionCall.Args,
						})
					}
				}
			}

			// Check if we're done
			if candidate.FinishReason != "" && candidate.FinishReason != genai.FinishReasonUnspecified {
				// Flush accumulated text
				if currentText.Len() > 0 {
					currentBlocks = append(currentBlocks, agent.TextContent{Text: currentText.String()})
					currentText.Reset()
				}

				// Add function calls
				for _, fc := range functionCallBuffers {
					currentBlocks = append(currentBlocks, agent.ToolCallContent{
						ID:        generateToolCallID(),
						Name:      fc.name,
						Arguments: fc.args,
					})
				}

				// Update final message
				if currentMessage != nil {
					currentMessage.Content = currentBlocks
					currentMessage.Timestamp = timestamp
				}

				eventCh <- agent.MessageEndEvent{
					Timestamp: timestamp,
					Message:   *currentMessage,
				}
				return
			}
		}
	}()

	return eventCh, nil
}

// convertMessagesToGenai converts agent messages to genai.Content format.
func (c *GoogleClient) convertMessagesToGenai(messages []agent.AgentMessage) []*genai.Content {
	result := make([]*genai.Content, 0, len(messages))

	for _, msg := range messages {
		parts := c.convertContentToGenaiParts(msg)

		var role string
		switch msg.Role {
		case agent.RoleUser:
			role = "user"
		case agent.RoleAssistant:
			role = "model"
		case agent.RoleToolResult:
			// Tool results are sent as user messages with FunctionResponse parts
			role = "user"
			parts = []*genai.Part{
				{
					FunctionResponse: &genai.FunctionResponse{
						Name:     msg.ToolName,
						Response: convertToolResultContent(msg.Content),
					},
				},
			}
		default:
			role = "user"
		}

		content := &genai.Content{
			Role:  role,
			Parts: parts,
		}
		result = append(result, content)
	}

	return result
}

// convertContentToGenaiParts converts message content to genai.Part format.
func (c *GoogleClient) convertContentToGenaiParts(msg agent.AgentMessage) []*genai.Part {
	var parts []*genai.Part

	for _, block := range msg.Content {
		switch b := block.(type) {
		case agent.TextContent:
			parts = append(parts, &genai.Part{Text: b.Text})
		case agent.ImageContent:
			parts = append(parts, &genai.Part{
				InlineData: &genai.Blob{
					MIMEType: b.MimeType,
					Data:     []byte(b.Data),
				},
			})
		case agent.ToolCallContent:
			// Convert tool call to function call part
			parts = append(parts, &genai.Part{
				FunctionCall: &genai.FunctionCall{
					Name: b.Name,
					Args: b.Arguments,
				},
			})
		}
	}

	return parts
}

// convertToolsToGenai converts agent tools to genai Tool format.
func (c *GoogleClient) convertToolsToGenai(tools []agent.Tool) []*genai.Tool {
	// Convert to Google's format first
	googleTools := ToGoogleTools(tools)

	// Then convert to genai format
	result := make([]*genai.Tool, len(googleTools.FunctionDeclarations))
	for i, gt := range googleTools.FunctionDeclarations {
		result[i] = &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{
				{
					Name:        gt.Name,
					Description: gt.Description,
					Parameters:  convertJSONSchemaToGenai(gt.Parameters),
				},
			},
		}
	}

	return result
}

// convertJSONSchemaToGenai converts JSONSchema to genai.Schema format.
func convertJSONSchemaToGenai(schema *JSONSchema) *genai.Schema {
	if schema == nil {
		return nil
	}

	genaiSchema := &genai.Schema{
		Type: genai.Type(schema.Type),
	}

	if len(schema.Properties) > 0 {
		properties := make(map[string]*genai.Schema)
		for name, prop := range schema.Properties {
			properties[name] = &genai.Schema{
				Type:        genai.Type(prop.Type),
				Description: prop.Description,
			}
		}
		genaiSchema.Properties = properties
	}

	if len(schema.Required) > 0 {
		genaiSchema.Required = schema.Required
	}

	return genaiSchema
}

// convertToolResultContent converts tool result content to a format suitable for FunctionResponse.
func convertToolResultContent(content []agent.ContentBlock) map[string]any {
	// Extract the result from content blocks
	if len(content) > 0 {
		if tc, ok := content[0].(agent.TextContent); ok {
			return map[string]any{"result": tc.Text}
		}
	}
	return map[string]any{"result": nil}
}

// functionCallBuffer accumulates function call data across streaming chunks.
type functionCallBuffer struct {
	name string
	args map[string]any
}

// generateToolCallID generates a unique ID for tool calls.
func generateToolCallID() string {
	return fmt.Sprintf("call_%d", time.Now().UnixNano())
}

// ConvertContentToText converts content blocks to a text string.
func ConvertContentToText(content []agent.ContentBlock) string {
	var result strings.Builder
	for _, block := range content {
		if tc, ok := block.(agent.TextContent); ok {
			result.WriteString(tc.Text)
		}
	}
	return result.String()
}
