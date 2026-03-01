# LLM Package

This package provides LLM provider abstractions and message conversion utilities for the gateway.

## Files

### models_config.go

Loads model configuration from models.json. Contains the JSON structure types and runtime types.

**Types:**
- `LLM` - Main entry struct with Config and ModelSelector
- `ModelsConfig` - JSON structure (Providers, NotDiamond, Router)
- `ModelConfig` - Per-model pricing/tier info
- `ProviderConfig` - Provider API settings
- `RuntimeModelConfig` - Flattened config for provider factory

**Functions:**
- `InitLLMModels(path)` - Initialize LLM (returns `*LLM` with Config + ModelSelector)
- `LoadModelsConfig(path)` - Low-level JSON loading
- `GetRuntimeModel(name)` - Get flattened config for provider creation
- `GetNotDiamondAPIKey/BaseURL()` - Access NotDiamond settings

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

### model_selector.go

NotDiamond model router integration.

**Features:**
- Selects optimal model based on message content
- Uses NotDiamond API for model recommendations
- Supports provider filtering and tradeoff preferences

**Status:** ✅ Complete

## Usage

```go
import "github.com/nevaria-ai/gateway/internal/llm"

// Initialize LLM (loads models.json and creates ModelSelector)
llmClient, err := llm.InitLLMModels("models.json")
if err != nil {
    log.Fatal(err)
}

// Access components
config := llmClient.Config           // ModelsConfig
selector := llmClient.ModelSelector  // NotDiamond router

// Create provider factory for chat model instances
factory := llm.NewProviderFactory(config)
chatModel, err := factory.CreateChatModel(ctx, "gpt-4o")

// Convert messages
agentMsgs := []agent.AgentMessage{...}
einoMsgs := llm.AgentMessagesToEinoMessages(agentMsgs)
```

## Configuration

Example models.json:

```json
{
  "providers": {
    "openai": {
      "api_key_env": "OPENAI_API_KEY",
      "base_url": "https://api.openai.com/v1",
      "models": {
        "gpt-4o": {
          "input_cost_per_million": 2.50,
          "output_cost_per_million": 10.00,
          "max_tokens": 128000,
          "tier": "standard"
        }
      }
    },
    "anthropic": {
      "api_key_env": "ANTHROPIC_API_KEY",
      "base_url": "https://api.anthropic.com",
      "models": {
        "claude-3-5-sonnet": {
          "input_cost_per_million": 3.00,
          "output_cost_per_million": 15.00,
          "max_tokens": 200000,
          "tier": "standard"
        }
      }
    }
  },
  "not_diamond": {
    "api_key_env": "NOT_DIAMOND_API_KEY",
    "base_url": "https://api.notdiamond.ai"
  }
}
```

## Tests

Test files are located in `tests/unit/config/` and `tests/unit/llm/`:

- `models_test.go` - Tests for configuration loading ✅ Passing
- `converter_test.go` - Tests for message conversion (blocked by dependencies)
- `provider_factory_test.go` - Tests for provider factory (blocked by dependencies)

## Dependencies

The package requires:
- `github.com/cloudwego/eino` - Core eino framework
- `github.com/cloudwego/eino-ext/components/model/openai` - OpenAI provider
- `github.com/cloudwego/eino-ext/components/model/claude` - Anthropic provider
- `github.com/cloudwego/eino-ext/components/model/gemini` - Google provider

**Note:** The eino-ext packages currently have invalid version strings in the project's go.mod (`v0.0.0-latest`). These need to be resolved before the provider factory can be fully implemented and tested.
