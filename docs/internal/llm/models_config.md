# Models Configuration

JSON-based LLM model and provider configuration. Defines model costs, provider settings, and NotDiamond router integration.

**Package:** `github.com/nevaria-ai/gateway/internal/llm`

## Overview

## Structs

### ModelsConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ModelsConfig` | `Providers` | `map[string]*ProviderConfig` | Provider name to config mapping |
| | `NotDiamond` | `*NotDiamondConfig` | NotDiamond router settings |
| | `Router` | `RouterConfig` | Default router configuration |

### ProviderConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ProviderConfig` | `APIKeyEnv` | `string` | Environment variable name for API key |
| | `BaseURL` | `string` | Provider API base URL |
| | `Models` | `map[string]*ModelConfig` | Model name to config mapping |

### ModelConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ModelConfig` | `InputCostPerMillion` | `float64` | Input token cost per million |
| | `OutputCostPerMillion` | `float64` | Output token cost per million |
| | `MaxTokens` | `int` | Maximum context tokens |
| | `Tier` | `string` | Pricing tier (economy, standard, premium) |

### NotDiamondConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `NotDiamondConfig` | `APIKeyEnv` | `string` | Environment variable name for API key |
| | `BaseURL` | `string` | NotDiamond API base URL |

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
    ReadFile --> Unmarshal["json.Unmarshal(data, &config) → err"]
    Unmarshal -->|error| EParse["return nil, err"]
    Unmarshal --> CheckRouter["config.Router.Model != \"\""]
    CheckRouter -->|false| Success["return &config, nil"]
    CheckRouter -->|true| Lookup["config.GetModel(config.Router.Model)"]
    Lookup -->|not found| ELookup["return nil, err"]
    Lookup -->|found| Success

    click ReadFile href "https://pkg.go.dev/os#ReadFile"
    click Unmarshal href "https://pkg.go.dev/encoding/json#Unmarshal"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Success mainFn
    class HLoadConfig internalFn
    class ReadFile,Unmarshal localPkgs
    class ERead,EParse,ELookup errorFlow
```

### InitLLMModels

Entry point for LLM initialization. Returns `*LLM` with both config and model selector.

```mermaid
flowchart TB
    Start([path string]) --> HInit["**InitLLMModels(path) → *LLM, err**"]
    HInit --> Load["LoadModelsConfig(path) → config, err"]
    Load -->|error| ELoad["return nil, err"]
    Load -->|ok| CreateSelector["NewModelSelector(config) → selector"]
    CreateSelector --> Return["return &LLM{Config, ModelSelector}, nil"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class HInit,Load,CreateSelector internalFn
    class ELoad errorFlow
```

### LLM

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `LLM` | `Config` | `*ModelsConfig` | Loaded models configuration |
| | `ModelSelector` | `*ModelSelector` | NotDiamond model router |

### GetModel

```mermaid
flowchart TB
    Start([c *ModelsConfig, name string]) --> HGetModel["**(c *ModelsConfig).GetModel(name) → cfg, exists**"]
    HGetModel --> Loop["for _, provider ← range c.Providers"]
    Loop --> Lookup["provider.Models[name]"]
    Lookup -->|exists| ReturnFound["return cfg, true"]
    Lookup -->|not found| Loop
    Loop -->|done| ReturnNotFound["return nil, false"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,ReturnFound,ReturnNotFound mainFn
    class HGetModel internalFn
```

### GetAllModels

```mermaid
flowchart TB
    Start([c *ModelsConfig]) --> HGetAll["**(c *ModelsConfig).GetAllModels() → names**"]
    HGetAll --> MakeNames["names ← make([]string, 0)"]
    MakeNames --> Loop["for _, provider ← range c.Providers"]
    Loop --> InnerLoop["for name ← range provider.Models"]
    InnerLoop --> Append["names = append(names, name)"]
    Append --> InnerLoop
    InnerLoop -->|done| Loop
    Loop -->|done| Return["return names"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class HGetAll internalFn
```

### GetProvider

```mermaid
flowchart TB
    Start([c *ModelsConfig, name string]) --> HGetProvider["**(c *ModelsConfig).GetProvider(name) → cfg, exists**"]
    HGetProvider --> Lookup["c.Providers[name]"]
    Lookup --> Return["return provider, exists"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class HGetProvider internalFn
```

### GetModelsForProvider

```mermaid
flowchart TB
    Start([c *ModelsConfig, providerName string]) --> HGetForProvider["**(c *ModelsConfig).GetModelsForProvider(providerName) → models**"]
    HGetForProvider --> Check["c.Providers[providerName]"]
    Check -->|not found| ReturnEmpty["return []"]
    Check -->|found| Loop["for _, model ← range provider.Models"]
    Loop --> Append["models = append(models, model)"]
    Append --> Loop
    Loop -->|done| Return["return models"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return,ReturnEmpty mainFn
    class HGetForProvider internalFn
```

### GetModelsByTier

```mermaid
flowchart TB
    Start([c *ModelsConfig, tier string]) --> HGetByTier["**(c *ModelsConfig).GetModelsByTier(tier) → models**"]
    HGetByTier --> MakeModels["models ← make([]*ModelConfig, 0)"]
    MakeModels --> Loop["for _, provider ← range c.Providers"]
    Loop --> InnerLoop["for _, model ← range provider.Models"]
    InnerLoop --> CheckTier["model.Tier == tier"]
    CheckTier -->|true| Append["models = append(models, model)"]
    Append --> InnerLoop
    CheckTier -->|false| InnerLoop
    InnerLoop -->|done| Loop
    Loop -->|done| Return["return models"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return mainFn
    class HGetByTier internalFn
```

### GetNotDiamondAPIKey

```mermaid
flowchart TB
    Start([c *ModelsConfig]) --> HGetKey["**(c *ModelsConfig).GetNotDiamondAPIKey() → apiKey**"]
    HGetKey --> Check["c.NotDiamond == nil OR c.NotDiamond.APIKeyEnv == \"\""]
    Check -->|true| ReturnEmpty["return \"\""]
    Check -->|false| GetEnv["os.Getenv(c.NotDiamond.APIKeyEnv)"]
    GetEnv --> Return["return apiKey"]

    click GetEnv href "https://pkg.go.dev/os#Getenv"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2

    class Start,Return,ReturnEmpty mainFn
    class HGetKey internalFn
    class GetEnv localPkgs
```

### GetNotDiamondBaseURL

```mermaid
flowchart TB
    Start([c *ModelsConfig]) --> HGetURL["**(c *ModelsConfig).GetNotDiamondBaseURL() → baseURL**"]
    HGetURL --> Check["c.NotDiamond == nil"]
    Check -->|true| ReturnEmpty["return \"\""]
    Check -->|false| Return["return c.NotDiamond.BaseURL"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Return,ReturnEmpty mainFn
    class HGetURL internalFn
```

## Design Notes

### JSON-Based Configuration

**Q: Why use JSON for model configuration?**

| Aspect | Description |
|--------|-------------|
| Context | Model costs and providers change frequently |
| Options considered | Code constants, JSON, YAML, Database |
| Rationale | JSON is simple, widely supported, and easy to parse |

**Current approach is recommended because:**
- Easy to add new models without code changes
- Cost updates don't require deployment
- Version controlled alongside code
- Native Go encoding/json support

### Env-Based API Keys

**Q: Why store API key env var names instead of keys directly?**

| Aspect | Description |
|--------|-------------|
| Context | API keys are sensitive secrets |
| Options considered | Store keys in config, env var names, separate secrets file |
| Rationale | Keeps secrets out of version control while allowing config flexibility |

**Current approach is recommended because:**
- Config file can be committed to git safely
- Keys injected at runtime via environment
- Standard 12-factor app pattern
- Easy to rotate keys without config changes

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestLoadModelsConfig` | Valid JSON loading |
| `TestGetModel` | Model retrieval across providers |
| `TestGetAllModels` | All models list |
| `TestGetProviderConfig` | Provider retrieval |
| `TestGetModelsForProvider` | Provider filtering |
| `TestGetModelsByTier` | Tier filtering |

**Running:**
```bash
make test-unit
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `encoding/json` | JSON parsing |
| `os` | Environment variable access |

**File Location:** [`internal/llm/models_config.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/llm/models_config.go)

## Related

- [Config README](README.md) - Main configuration
- [Plans](plans.md) - Subscription plan definitions
