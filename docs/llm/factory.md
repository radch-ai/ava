# LLM Factory

This document describes the factory pattern implementation for creating LLM clients and detecting providers from model names.

## Client

```go
type Client struct {
    provider  Provider
    openai    *openaiClient
    anthropic *anthropicClient
    google    *googleClient
    model     agent.Model
}
```

The unified `Client` wraps all provider-specific clients and delegates to the appropriate implementation based on the detected provider.

### Methods

#### GetProvider

```go
func (c *Client) GetProvider() Provider
```

Returns the provider for this client.

#### GetModel

```go
func (c *Client) GetModel() agent.Model
```

Returns the model configuration for this client.

#### Stream

```go
func (c *Client) Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error)
```

Sends messages to the LLM and returns a channel of events. Delegates to the appropriate provider-specific client.

## NewClient

```go
func NewClient(config Config, model agent.Model) (*Client, error)
```

Creates a new LLM client based on model configuration.

### Provider Selection Logic

1. If `model.Provider` is set, use that provider
2. Otherwise, auto-detect from `model.ID` using `DetectProviderFromModel`
3. Initialize the appropriate provider-specific client

### Example

```go
config := llm.Config{
    OpenAIAPIKey: os.Getenv("OPENAI_API_KEY"),
    Timeout:      120 * time.Second,
}

// With explicit provider
model := agent.Model{
    ID:       "custom-model",
    Provider: "openai",
}
client, err := llm.NewClient(config, model)

// With auto-detection
model := agent.Model{
    ID: "gpt-4o", // Auto-detected as OpenAI
}
client, err := llm.NewClient(config, model)
```

## DetectProviderFromModel

```go
func DetectProviderFromModel(model string) (Provider, error)
```

Detects the provider from a model name using pattern matching.

### Detection Rules

The function checks `ModelProviderMapping` for prefix matches:

| Model Pattern | Provider |
|--------------|----------|
| `gpt-4`, `gpt-3.5`, `gpt-4o`, `gpt-4-turbo` | OpenAI |
| `o1-` | OpenAI (o1-series models) |
| `claude-` | Anthropic |
| `gemini-` | Google |

### Returns

- `Provider` - The detected provider
- `error` - `ErrUnknownModel` if the model cannot be matched to a provider

### Example

```go
provider, err := llm.DetectProviderFromModel("gpt-4o")
// Returns: ProviderOpenAI, nil

provider, err := llm.DetectProviderFromModel("claude-3-5-sonnet")
// Returns: ProviderAnthropic, nil

provider, err := llm.DetectProviderFromModel("unknown-model")
// Returns: "", ErrUnknownModel
```

## ProviderFromString

```go
func ProviderFromString(s string) (Provider, error)
```

Parses a provider from a string (case-insensitive).

### Example

```go
provider, err := llm.ProviderFromString("openai")
// Returns: ProviderOpenAI, nil

provider, err := llm.ProviderFromString("OpenAI")
// Returns: ProviderOpenAI, nil

provider, err := llm.ProviderFromString("unknown")
// Returns: "", ErrUnknownProvider
```

## Errors

```go
var (
    ErrMissingAPIKey  = errors.New("missing API key for provider")
    ErrUnknownProvider = errors.New("unknown LLM provider")
    ErrUnknownModel    = errors.New("cannot determine provider for model")
)
```

- `ErrMissingAPIKey` - Returned when the required API key for a provider is not configured
- `ErrUnknownProvider` - Returned when parsing an invalid provider string
- `ErrUnknownModel` - Returned when a model name cannot be matched to any provider

## Provider-Specific Wrappers

The factory creates internal wrappers for each provider:

### openaiClient

```go
type openaiClient struct {
    *OpenAIClient
}
```

Wraps `OpenAIClient` to satisfy the factory interface.

### anthropicClient

```go
type anthropicClient struct {
    *AnthropicClient
}
```

Wraps `AnthropicClient` to satisfy the factory interface.

### googleClient

```go
type googleClient struct {
    *GoogleClient
}
```

Wraps `GoogleClient` to satisfy the factory interface.

## Related

- [Types](types.md) - Provider enum and Config struct
- [OpenAI Client](openai.md) - OpenAI implementation details
- [Anthropic Client](anthropic.md) - Anthropic implementation details
- [Google Client](google.md) - Google implementation details
