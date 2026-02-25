# Models Configuration

YAML-based LLM model and router configuration. Provides model cost definitions, provider mappings, and router settings.

## Overview

<!-- Overview replaced by individual function flowcharts below -->

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

### LoadModelsConfig

```mermaid
flowchart TB
    Start([path string]) --> HLoadConfig["**LoadModelsConfig(path) → config, err**"]
    HLoadConfig --> ReadFile["os.ReadFile(path) → data, err"]
    ReadFile -->|error| ERead["return nil, err"]
    ReadFile --> Unmarshal["yaml.Unmarshal(data, &config) → err"]
    Unmarshal -->|error| EParse["return nil, err"]
    Unmarshal --> CheckRouter["config.Router.Model != \"\""]
    CheckRouter -->|false| Success["return &config, nil"]
    CheckRouter -->|true| Lookup["config.Models[config.Router.Model]"]
    Lookup -->|not found| ELookup["return nil, err"]
    Lookup -->|found| Success

    click ReadFile href "https://pkg.go.dev/os#ReadFile"
    click Unmarshal href "https://pkg.go.dev/gopkg.in/yaml.v3#Unmarshal"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Success mainFn
    class HLoadConfig internalFn
    class ReadFile,Unmarshal localPkgs
    class ERead,EParse,ELookup errorFlow
```

### GetModel

```mermaid
flowchart TB
    Start([c *ModelsConfig, name string]) --> HGetModel["**(c *ModelsConfig).GetModel(name) → cfg, exists**"]
    HGetModel --> Lookup["c.Models[name]"]
    Lookup --> Assign["cfg ← value"]
    Assign --> Return["return cfg, exists"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class HGetModel internalFn
```

### GetAllModels

```mermaid
flowchart TB
    Start([c *ModelsConfig]) --> HGetAll["**(c *ModelsConfig).GetAllModels() → names**"]
    HGetAll --> MakeNames["names ← make([]string, 0, len(c.Models))"]
    MakeNames --> Loop["for name ← range c.Models"]
    Loop --> Append["names = append(names, name)"]
    Append --> Loop
    Loop -->|done| Return["return names"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class HGetAll internalFn
```

### GetModelsByProvider

```mermaid
flowchart TB
    Start([c *ModelsConfig, provider string]) --> HGetByProvider["**(c *ModelsConfig).GetModelsByProvider(provider) → models**"]
    HGetByProvider --> MakeModels["models ← make([]*ModelConfig, 0)"]
    MakeModels --> Loop["for cfg ← range c.Models"]
    Loop --> CheckProvider["cfg.Provider == provider"]
    CheckProvider -->|true| Append["models = append(models, cfg)"]
    Append --> Loop
    CheckProvider -->|false| Loop
    Loop -->|done| Return["return models"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class HGetByProvider internalFn
```

### GetModelsByTier

```mermaid
flowchart TB
    Start([c *ModelsConfig, tier string]) --> HGetByTier["**(c *ModelsConfig).GetModelsByTier(tier) → models**"]
    HGetByTier --> MakeModels["models ← make([]*ModelConfig, 0)"]
    MakeModels --> Loop["for cfg ← range c.Models"]
    Loop --> CheckTier["cfg.Tier == tier"]
    CheckTier -->|true| Append["models = append(models, cfg)"]
    Append --> Loop
    CheckTier -->|false| Loop
    Loop -->|done| Return["return models"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class HGetByTier internalFn
```

## Design Notes

### YAML-Based Configuration## Design Notes

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
