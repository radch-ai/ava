# LLM Package

This package provides LLM provider abstractions and message conversion utilities for the gateway.

## Files

### config.go

Loads model configuration from models.json.

**Features:**
- Loads model configurations from JSON file
- Supports multiple providers (OpenAI, Anthropic, Google, Grok)
- Provides API key retrieval from environment variables
- Converts config.ModelsConfig to llm.ModelsConfig

**Status:** ✅ Complete and tested

### converter.go
Converts between agent.AgentMessage and eino schema.Message.

**Features:**
- Converts agent messages to eino messages
- Converts eino messages to agent messages  
- Handles all message types (user, assistant, tool result)
- Supports tool calls and tool results
- Handles multimodal content (text, images)

**Status:** ✅ Implementation complete, tests blocked by eino-ext dependency issues

### provider_factory.go
Creates eino ChatModel instances with caching.

**Features:**
- Factory pattern for creating chat models
- Support for multiple LLM providers:
  - OpenAI
  - Anthropic (Claude)
  - Google (Gemini)
  - Grok (OpenAI-compatible with x.ai base URL)
- Caching of created model instances
- Thread-safe operations

**Status:** 🚧 Structure complete, implementation blocked by eino-ext dependency issues

**Note:** The provider factory methods (createOpenAIModel, createAnthropicModel, createGeminiModel, createGrokModel) currently return error messages because the required eino-ext packages are not properly set up in the project's go.mod. Once the dependencies are resolved, these methods should be implemented as:

```go
func (f *ProviderFactory) createOpenAIModel(ctx context.Context, config *ModelConfig, apiKey string) (model.ChatModel, error) {
    cfg := &openaiclient.Config{
        APIKey: apiKey,
        Model:  config.ModelID,
    }
    if config.BaseURL != "" {
        cfg.BaseURL = config.BaseURL
    }
    return openaiclient.NewChatModel(ctx, cfg)
}
```

## Usage

```go
import (
    "github.com/nevaria-ai/gateway/config"
    "github.com/nevaria-ai/gateway/internal/llm"
)

// Load models config (from config package)
modelsCfg, err := config.InitLLMModels("models.json")
if err != nil {
    log.Fatal(err)
}

// Create model selector
selector := llm.NewModelSelector(modelsCfg.GetNotDiamondBaseURL(), modelsCfg.GetNotDiamondAPIKey())

// Convert messages
agentMsgs := []agent.AgentMessage{...}
einoMsgs := llm.AgentMessagesToEinoMessages(agentMsgs)
```

## Configuration

Example models.json:

```json
{
  "models": {
    "gpt-4o": {
      "provider": "openai",
      "model_id": "gpt-4o",
      "base_url": "https://api.openai.com/v1",
      "api_key_env": "OPENAI_API_KEY"
    },
    "claude-3-5-sonnet": {
      "provider": "anthropic",
      "model_id": "claude-3-5-sonnet-20241022",
      "base_url": "https://api.anthropic.com",
      "api_key_env": "ANTHROPIC_API_KEY"
    },
    "gemini-2.0-flash": {
      "provider": "google",
      "model_id": "gemini-2.0-flash-exp",
      "base_url": "https://generativelanguage.googleapis.com/v1beta",
      "api_key_env": "GOOGLE_API_KEY"
    },
    "grok-2": {
      "provider": "grok",
      "model_id": "grok-2",
      "base_url": "https://api.x.ai/v1",
      "api_key_env": "XAI_API_KEY"
    }
  }
}
```

## Tests

Test files are located in `tests/unit/llm/`:

- `config_test.go` - Tests for configuration loading ✅ Passing
- `converter_test.go` - Tests for message conversion (blocked by dependencies)
- `provider_factory_test.go` - Tests for provider factory (blocked by dependencies)

## Dependencies

The package requires:
- `github.com/cloudwego/eino` - Core eino framework
- `github.com/cloudwego/eino-ext/components/model/openai` - OpenAI provider
- `github.com/cloudwego/eino-ext/components/model/claude` - Anthropic provider
- `github.com/cloudwego/eino-ext/components/model/gemini` - Google provider

**Note:** The eino-ext packages currently have invalid version strings in the project's go.mod (`v0.0.0-latest`). These need to be resolved before the provider factory can be fully implemented and tested.

## Next Steps

1. Resolve eino-ext dependency issues in go.mod
2. Implement provider creation methods in provider_factory.go
3. Run and fix converter and provider_factory tests
4. Add integration tests with actual LLM providers
5. Add streaming support
6. Add error handling and retry logic
