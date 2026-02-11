# LLM Client Framework

The LLM client framework provides a unified interface for integrating with various LLM providers. It abstracts the differences between providers while maintaining provider-specific optimizations and supports streaming responses with comprehensive event handling.

## Overview

- **Unified Interface**: Consistent API across all LLM providers (OpenAI, Anthropic, Google)
- **Provider Abstraction**: Transparent switching between providers with auto-detection
- **Streaming Support**: Real-time streaming responses with event notifications
- **Configuration Management**: Flexible configuration per provider with environment variable support
- **Error Handling**: Comprehensive error handling with retry logic and provider-specific error types
- **Factory Pattern**: Dynamic client creation with provider detection

## Documentation

| Document | Description |
|----------|-------------|
| [types.md](types.md) | Provider enum, ModelProviderMapping, Config struct |
| [factory.md](factory.md) | Client struct, NewClient, DetectProviderFromModel, Stream |
| [tools.md](tools.md) | JSON Schema types, ToOpenAITool, ToAnthropicTool, ToGoogleTool |
| [openai.md](openai.md) | OpenAIClient, streaming, SSE parsing, Models (gpt-4o, o1, etc) |
| [anthropic.md](anthropic.md) | AnthropicClient, streaming, thinking support, Models (claude-*) |
| [google.md](google.md) | GoogleClient, streaming, Models (gemini-2.5-pro, gemini-2.5-flash) |

## Quick Start

```go
import (
    "github.com/nevaria-ai/gateway/internal/llm"
    "github.com/nevaria-ai/gateway/internal/agent"
)

// Configure the client
config := llm.Config{
    OpenAIAPIKey:     os.Getenv("OPENAI_API_KEY"),
    AnthropicAPIKey:  os.Getenv("ANTHROPIC_API_KEY"),
    GoogleAPIKey:     os.Getenv("GOOGLE_API_KEY"),
    Timeout:          120 * time.Second,
}

// Create a client (auto-detects provider from model ID)
model := agent.Model{ID: "gpt-4o"} // or "claude-3-5-sonnet", "gemini-2.5-pro"
client, err := llm.NewClient(config, model)
if err != nil {
    log.Fatal(err)
}

// Stream a response
events, err := client.Stream(ctx, messages, tools)
if err != nil {
    log.Fatal(err)
}

for event := range events {
    switch e := event.(type) {
    case agent.MessageStartEvent:
        fmt.Printf("Assistant: %s", e.Message.Content)
    case agent.MessageUpdateEvent:
        fmt.Print(e.Delta)
    }
}
```

## Architecture

### Unified Client Interface

```go
type LLMClient interface {
    Stream(ctx context.Context, messages []agent.AgentMessage, tools []agent.Tool) (<-chan agent.AgentEvent, error)
    GetProvider() Provider
    GetModel() agent.Model
}
```

The `llm.Client` type implements this interface, providing a single entry point for all provider operations.

### Provider Registry

```go
type Provider string

const (
    ProviderOpenAI     Provider = "openai"
    ProviderAnthropic  Provider = "anthropic"
    ProviderGoogle     Provider = "google"
)
```

### Auto-Detection

The framework automatically detects the provider from model name patterns:

- `gpt-`, `o1-` (including gpt-4o, gpt-4-turbo) -> OpenAI
- `claude-` (including claude-3-5-sonnet, claude-3-opus) -> Anthropic
- `gemini-` (including gemini-2.5-pro, gemini-2.5-flash) -> Google

## Environment Variables

```bash
# OpenAI
OPENAI_API_KEY=sk-...
OPENAI_BASE_URL=https://api.openai.com/v1

# Anthropic
ANTHROPIC_API_KEY=sk-ant-...
ANTHROPIC_BASE_URL=https://api.anthropic.com

# Google
GOOGLE_API_KEY=...
GOOGLE_BASE_URL=https://generativelanguage.googleapis.com
```

## Related Documentation

- [Agent Framework](../agent/README.md) - Agent framework integration
- [WebSocket Handler](../websocket.md) - WebSocket communication
