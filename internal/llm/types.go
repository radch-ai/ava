// Package llm provides unified types and factory for managing multiple LLM providers.
package llm

import "time"

// Provider represents an LLM provider (OpenAI, Anthropic, Google, etc.).
type Provider string

const (
	// ProviderOpenAI is the OpenAI provider.
	ProviderOpenAI Provider = "openai"
	// ProviderAnthropic is the Anthropic provider.
	ProviderAnthropic Provider = "anthropic"
	// ProviderGoogle is the Google provider.
	ProviderGoogle Provider = "google"
)

// String returns the string representation of the provider.
func (p Provider) String() string {
	return string(p)
}

// DefaultTimeout is the default timeout for LLM API requests.
const DefaultTimeout = 120 * time.Second

// Config holds configuration for all LLM providers.
type Config struct {
	// API keys for each provider
	OpenAIAPIKey    string
	AnthropicAPIKey string
	GoogleAPIKey    string

	// Custom base URLs (optional, for proxy/testing)
	OpenAIBaseURL    string
	AnthropicBaseURL string
	GoogleBaseURL    string

	// Request timeout
	Timeout time.Duration
}

// ApplyDefaults applies default values to the config if not set.
func (c *Config) ApplyDefaults() Config {
	config := *c // Copy

	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}

	return config
}

// ModelProviderMapping maps model name patterns to their providers.
var ModelProviderMapping = map[string]Provider{
	// OpenAI models
	"gpt-4":       ProviderOpenAI,
	"gpt-3.5":     ProviderOpenAI,
	"gpt-4o":      ProviderOpenAI,
	"gpt-4-turbo": ProviderOpenAI,
	"o1-":         ProviderOpenAI, // o1-series models

	// Anthropic models
	"claude-": ProviderAnthropic,

	// Google models
	"gemini-": ProviderGoogle,
}
