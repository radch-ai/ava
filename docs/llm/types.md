# LLM Types

This document describes the core types and configuration structures used across the LLM client framework.

## Provider

```go
type Provider string
```

`Provider` represents an LLM provider (OpenAI, Anthropic, Google).

### Constants

```go
const (
    ProviderOpenAI     Provider = "openai"
    ProviderAnthropic  Provider = "anthropic"
    ProviderGoogle     Provider = "google"
)
```

### Methods

#### String

```go
func (p Provider) String() string
```

Returns the string representation of the provider.

## Config

```go
type Config struct {
    // API keys for each provider
    OpenAIAPIKey     string
    AnthropicAPIKey  string
    GoogleAPIKey     string

    // Custom base URLs (optional, for proxy/testing)
    OpenAIBaseURL     string
    AnthropicBaseURL  string
    GoogleBaseURL     string

    // Request timeout
    Timeout time.Duration
}
```

`Config` holds configuration for all LLM providers.

### ApplyDefaults

```go
func (c *Config) ApplyDefaults() Config
```

Applies default values to the config if not set. Currently sets `Timeout` to `DefaultTimeout` (120 seconds) if zero.

## ModelProviderMapping

```go
var ModelProviderMapping = map[string]Provider{
    // OpenAI models
    "gpt-4":          ProviderOpenAI,
    "gpt-3.5":        ProviderOpenAI,
    "gpt-4o":         ProviderOpenAI,
    "gpt-4-turbo":    ProviderOpenAI,
    "o1-":            ProviderOpenAI, // o1-series models

    // Anthropic models
    "claude-": ProviderAnthropic,

    // Google models
    "gemini-": ProviderGoogle,
}
```

Maps model name patterns to their providers. This mapping is used by `DetectProviderFromModel` for auto-detection.

## DefaultTimeout

```go
const DefaultTimeout = 120 * time.Second
```

Default timeout for LLM API requests.

## Usage Example

```go
config := llm.Config{
    OpenAIAPIKey:    os.Getenv("OPENAI_API_KEY"),
    AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
    GoogleAPIKey:     os.Getenv("GOOGLE_API_KEY"),
    Timeout:         120 * time.Second,
}

// Apply defaults (will set timeout if 0)
config = config.ApplyDefaults()

// Create client with model
model := agent.Model{
    ID:       "gpt-4o",
    Provider:  "openai",
}

client, err := llm.NewClient(config, model)
```

## Related

- [Factory](factory.md) - Client creation and provider detection
- [Provider Documentation](openai.md) - OpenAI-specific types
- [Provider Documentation](anthropic.md) - Anthropic-specific types
- [Provider Documentation](google.md) - Google-specific types
