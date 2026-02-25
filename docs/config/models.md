# Models Configuration

YAML-based LLM model and router configuration. Provides model cost definitions, provider mappings, and router settings.

## Overview

```mermaid
flowchart TB
    subgraph SGMain [" "]
        HStart["**LoadModelsConfig(path) → config, err**"]
        HStart --> ReadFile["os.ReadFile(path) → data"]
        ReadFile -->|error| ERead["return nil, err"]
        ReadFile --> ParseYAML["yaml.Unmarshal(data, &config)"]
        ParseYAML -->|error| EParse["return nil, err"]
        ParseYAML --> Validate["config.Router.Model != \"\""]
        Validate -->|false| Success["return &config, nil"]
        Validate -->|true| Lookup["config.Models[config.Router.Model]"]
        Lookup -->|not found| ELookup["return nil, err"]
        Lookup -->|found| Success
    end

    click ReadFile href "https://pkg.go.dev/os#ReadFile"
    click ParseYAML href "https://pkg.go.dev/gopkg.in/yaml.v3#Unmarshal"

    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class HStart internalFn
    class ReadFile,ParseYAML localPkgs
    class ERead,EParse,ELookup,Success errorFlow
```

## Structs

### ModelsConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ModelsConfig` | `Models` | `map[string]*ModelConfig` | Model name to config mapping |
| | `Router` | `RouterConfig` | Router configuration |

### ModelConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ModelConfig` | `Provider` | `string` | Provider name (openai, anthropic, google) |
| | `InputCostPerMillion` | `float64` | Input token cost per million |
| | `OutputCostPerMillion` | `float64` | Output token cost per million |
| | `MaxTokens` | `int` | Maximum context tokens |
| | `Tier` | `string` | Pricing tier (economy, standard, premium) |

### RouterConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RouterConfig` | `Model` | `string` | Model for routing decisions |
| | `Timeout` | `string` | Router request timeout |
| | `MaxRetries` | `int` | Maximum retry attempts |

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `LoadModelsConfig` | `(path string) (*ModelsConfig, error)` | Loads config from YAML file |
| `GetModel` | `(c *ModelsConfig, name string) (*ModelConfig, bool)` | Returns model by name |
| `GetAllModels` | `(c *ModelsConfig) []string` | Returns all model names |
| `GetModelsByProvider` | `(c *ModelsConfig, provider string) []*ModelConfig` | Returns models for provider |
| `GetModelsByTier` | `(c *ModelsConfig, tier string) []*ModelConfig` | Returns models in tier |

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `GATEWAY_MODEL_CONFIG` | No | `config/models.yaml` | Path to model config YAML |

## Design Notes

### YAML-Based Configuration

**Q: Why use YAML for model configuration?**

| Aspect | Description |
|--------|-------------|
| Context | Model costs and providers change frequently |
| Options considered | Code constants, JSON, YAML, Database |
| Rationale | YAML is human-readable and easy to update without recompiling |

**Current approach is recommended because:**
- Easy to add new models without code changes
- Cost updates don't require deployment
- Version controlled alongside code
- Supports comments for documentation

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestLoadModelsConfig` | Valid YAML loading |
| `TestLoadModelsConfig_FileNotFound` | Missing file handling |
| `TestLoadModelsConfig_InvalidYAML` | Parse error handling |
| `TestLoadModelsConfig_MissingRouterModel` | Validation error |
| `TestGetModel` | Model retrieval |
| `TestGetAllModels` | All models list |
| `TestGetModelsByProvider` | Provider filtering |
| `TestGetModelsByTier` | Tier filtering |

**Running:**
```bash
make test ./config
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `gopkg.in/yaml.v3` | YAML parsing |

**File Location:** [`config/models.go`](https://github.com/nevaria-ai/gateway/blob/main/config/models.go)

## Related

- [Config README](README.md) - Main configuration
- [Plans](plans.md) - Subscription plan definitions
- [LLM Proxy Models](../internal/llm-proxy/models.md) - Model registry usage
