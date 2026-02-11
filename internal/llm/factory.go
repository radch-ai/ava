// Package llm provides a unified factory for creating LLM clients.
package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nevaria-ai/gateway/internal/agent"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

var (
	// ErrMissingAPIKey is returned when the required API key is missing.
	ErrMissingAPIKey = errors.New("missing API key for provider")
	// ErrUnknownProvider is returned when the provider is unknown.
	ErrUnknownProvider = errors.New("unknown LLM provider")
	// ErrUnknownModel is returned when the model cannot be matched to a provider.
	ErrUnknownModel = errors.New("cannot determine provider for model")
)

// ProviderFromString parses a provider from a string.
func ProviderFromString(s string) (Provider, error) {
	switch strings.ToLower(s) {
	case string(ProviderOpenAI):
		return ProviderOpenAI, nil
	case string(ProviderAnthropic):
		return ProviderAnthropic, nil
	case string(ProviderGoogle):
		return ProviderGoogle, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownProvider, s)
	}
}

// DetectProviderFromModel detects the provider from a model name.
func DetectProviderFromModel(model string) (Provider, error) {
	if model == "" {
		return "", fmt.Errorf("%w: empty model name", ErrUnknownModel)
	}

	modelLower := strings.ToLower(model)

	// Check against known patterns
	for pattern, provider := range ModelProviderMapping {
		if strings.HasPrefix(modelLower, pattern) {
			return provider, nil
		}
	}

	return "", fmt.Errorf("%w: %s", ErrUnknownModel, model)
}

// Client is a unified client that wraps all provider clients.
type Client struct {
	provider  Provider
	openai    *openaiClient
	anthropic *anthropicClient
	google    *googleClient
	model     agent.Model
}

// NewClient creates a new LLM client based on the model configuration.
func NewClient(config Config, model agent.Model) (*Client, error) {
	config = config.ApplyDefaults()

	// Determine provider
	provider := Provider(model.Provider)
	if provider == "" {
		// Auto-detect from model ID
		var err error
		provider, err = DetectProviderFromModel(model.ID)
		if err != nil {
			return nil, err
		}
	}

	client := &Client{
		provider: provider,
		model:    model,
	}

	// Initialize provider-specific client
	switch provider {
	case ProviderOpenAI:
		if config.OpenAIAPIKey == "" {
			return nil, fmt.Errorf("%w: OpenAI", ErrMissingAPIKey)
		}
		client.openai = newOpenAIClient(config, model)

	case ProviderAnthropic:
		if config.AnthropicAPIKey == "" {
			return nil, fmt.Errorf("%w: Anthropic", ErrMissingAPIKey)
		}
		client.anthropic = newAnthropicClientWrapper(config, model)

	case ProviderGoogle:
		if config.GoogleAPIKey == "" {
			return nil, fmt.Errorf("%w: Google", ErrMissingAPIKey)
		}
		client.google = newGoogleClient(config, model)

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, provider)
	}

	return client, nil
}

// Stream sends messages to the LLM and returns a channel of events.
// This implements the agent.LLMClient interface.
func (c *Client) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	switch c.provider {
	case ProviderOpenAI:
		return c.openai.Stream(ctx, messages, tools)
	case ProviderAnthropic:
		return c.anthropic.Stream(ctx, messages, tools)
	case ProviderGoogle:
		return c.google.Stream(ctx, messages, tools)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, c.provider)
	}
}

// GetProvider returns the provider for this client.
func (c *Client) GetProvider() Provider {
	return c.provider
}

// GetModel returns the model configuration for this client.
func (c *Client) GetModel() agent.Model {
	return c.model
}

// openaiClient wraps the OpenAI provider implementation.
type openaiClient struct {
	*OpenAIClient
}

func newOpenAIClient(config Config, model agent.Model) *openaiClient {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	// Create OpenAI SDK client
	oaiClient := openai.NewClient(
		option.WithAPIKey(config.OpenAIAPIKey),
		option.WithBaseURL(config.OpenAIBaseURL),
		option.WithRequestTimeout(time.Duration(timeout)),
	)

	// Create our OpenAI client wrapper
	wrapper := NewOpenAIClient(OpenAIClientConfig{
		Client: oaiClient,
		Model:  model,
	})

	return &openaiClient{
		OpenAIClient: wrapper,
	}
}

// Stream implements the LLM streaming interface for OpenAI.
func (c *openaiClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	return c.OpenAIClient.Stream(ctx, messages, tools)
}

// googleClient wraps GoogleClient to satisfy the factory interface.
type googleClient struct {
	*GoogleClient
}

func newGoogleClient(config Config, model agent.Model) *googleClient {
	client := NewGoogleClient(GoogleClientConfig{
		APIKey:  config.GoogleAPIKey,
		BaseURL: config.GoogleBaseURL,
		Model:   model.ID,
		Timeout: config.Timeout,
	})

	return &googleClient{
		GoogleClient: client,
	}
}

// Stream implements the LLM streaming interface for Google.
func (c *googleClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	return c.GoogleClient.Stream(ctx, messages, tools)
}

// mockStreamResponse provides a mock streaming response for testing.
// This will be replaced with actual API implementations.
//
//nolint:unused // Kept for future use in testing
func mockStreamResponse(ctx context.Context, messages []agent.AgentMessage) (<-chan agent.AgentEvent, error) {
	ch := make(chan agent.AgentEvent, 10)

	go func() {
		defer close(ch)

		select {
		case <-ctx.Done():
			return
		default:
		}

		// Get last message for context
		var lastMsg agent.AgentMessage
		if len(messages) > 0 {
			lastMsg = messages[len(messages)-1]
		}

		// Create a simple response
		responseText := "Mock response"
		if len(lastMsg.Content) > 0 {
			if tc, ok := lastMsg.Content[0].(agent.TextContent); ok {
				responseText = "Mock response to: " + tc.Text
			}
		}

		responseMsg := agent.NewTextMessage(agent.RoleAssistant, responseText)

		// Emit turn start
		ch <- agent.TurnStartEvent{
			Timestamp: responseMsg.Timestamp,
		}

		// Emit message start
		ch <- agent.MessageStartEvent{
			Timestamp: responseMsg.Timestamp,
			Message:   responseMsg,
		}

		// Emit message update
		ch <- agent.MessageUpdateEvent{
			Timestamp: responseMsg.Timestamp,
			Message:   responseMsg,
			Delta:     responseText,
		}

		// Emit message end
		ch <- agent.MessageEndEvent{
			Timestamp: responseMsg.Timestamp,
			Message:   responseMsg,
		}

		// Emit turn end
		ch <- agent.TurnEndEvent{
			Timestamp: responseMsg.Timestamp,
		}
	}()

	return ch, nil
}

// anthropicClient wraps AnthropicClient to satisfy the factory interface.
type anthropicClient struct {
	*AnthropicClient
}

func newAnthropicClientWrapper(config Config, model agent.Model) *anthropicClient {
	client := NewAnthropicClient(AnthropicClientConfig{
		APIKey:        config.AnthropicAPIKey,
		BaseURL:       config.AnthropicBaseURL,
		Model:         model.ID,
		Timeout:       config.Timeout,
		ThinkingLevel: agent.ThinkingLevelOff, // Default, can be configured later
	})

	return &anthropicClient{
		AnthropicClient: client,
	}
}

// Stream implements the LLM streaming interface for Anthropic.
func (c *anthropicClient) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error) {
	return c.AnthropicClient.Stream(ctx, messages, tools)
}
