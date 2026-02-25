# LLM Proxy Models

Model registry with cost calculation and metadata.

## Overview

```mermaid
flowchart TB
    StartGet(["Get()"]) --> HOnce

    subgraph SGOnce [" "]
        HOnce["**sync.Once.Do(func)**"]
        HOnce --> HNewRegistry
    end

    subgraph SGNewRegistry [" "]
        HNewRegistry["**NewRegistry() → r**"]
        HNewRegistry --> DoneGet(["return r"])
    end

    StartLoad(["LoadFromYAML(path)"]) --> HReadFile

    subgraph SGReadFile [" "]
        HReadFile["**os.ReadFile(path) → data, err**"]
        HReadFile -->|error| E1["return err"]
        HReadFile --> HUnmarshal
    end

    subgraph SGUnmarshal [" "]
        HUnmarshal["**yaml.Unmarshal(data, &config) → err**"]
        HUnmarshal -->|error| E2["return err"]
        HUnmarshal --> HStoreModels
    end

    subgraph SGStoreModels [" "]
        HStoreModels["**r.models[name] = cfg**"]
        HStoreModels --> HStoreRouter
    end

    subgraph SGStoreRouter [" "]
        HStoreRouter["**r.router = config.Router**"]
        HStoreRouter --> DoneLoad(["return nil"])
    end

    StartCost(["GetCost(name, inputTokens, outputTokens)"]) --> HGetModel

    subgraph SGGetModel [" "]
        HGetModel["**GetModel(name) → cfg, ok**"]
        HGetModel -->|!ok| E3["return error"]
        HGetModel --> HCalculate
    end

    subgraph SGCalculate [" "]
        HCalculate["**CalculateCost(cfg, inputTokens, outputTokens) → cost**"]
        HCalculate --> DoneCost(["return cost"])
    end

    ReadFile["os.ReadFile(path) → data, err"]
    Unmarshal["yaml.Unmarshal(data, &config) → err"]

    click ReadFile href "https://pkg.go.dev/os#ReadFile"
    click Unmarshal href "https://pkg.go.dev/gopkg.in/yaml.v3#Unmarshal"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class StartGet,DoneGet,StartLoad,DoneLoad,StartCost,DoneCost mainFn
    class ReadFile,Unmarshal localPkgs
    class E1,E2,E3 errorFlow
```

## Structs

### Registry

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Registry` | `mu` | `sync.RWMutex` | Thread-safe lock |
| | `models` | `map[string]*ModelConfig` | Model storage |
| | `router` | `RouterConfig` | Router configuration |

### RouterConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RouterConfig` | `Model` | `string` | Model for routing |
| | `Timeout` | `string` | Timeout duration |

### ModelConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ModelConfig` | `Name` | `string` | Model name |
| | `Provider` | `string` | Provider name |
| | `InputCostPerMillion` | `float64` | Input token cost |
| | `OutputCostPerMillion` | `float64` | Output token cost |
| | `MaxTokens` | `int` | Maximum context |
| | `Tier` | `ModelTier` | Pricing tier |

### ModelTier

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `TierEconomy` | `ModelTier` | `"economy"` | Economy tier |
| `TierPremium` | `ModelTier` | `"premium"` | Premium tier |
| `TierUltra` | `ModelTier` | `"ultra"` | Ultra tier |

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `Get` | `() *Registry` | Returns singleton registry |
| `NewRegistry` | `() *Registry` | Creates new registry |
| `LoadFromYAML` | `(path string) error` | Loads models from YAML |
| `GetModel` | `(name string) (*ModelConfig, bool)` | Gets model by name |
| `GetRouterModel` | `() string` | Returns router model |
| `GetAllModels` | `() []string` | Returns all model names |
| `GetModelsByProvider` | `(provider string) []*ModelConfig` | Returns provider's models |
| `GetModelsByTier` | `(tier ModelTier) []*ModelConfig` | Returns tier's models |
| `CalculateCost` | `(cfg *ModelConfig, inputTokens, outputTokens int) float64` | Calculates cost |
| `EstimateCost` | `(cfg *ModelConfig, estimate *TokenEstimate) *CostEstimate` | Estimates cost from token estimate |

## Design Notes

### Singleton Pattern

**Q: Why use singleton for registry?**

| Aspect | Description |
|--------|-------------|
| Context | Model registry lifecycle |
| Options considered | Singleton, dependency injection, global |
| Rationale | Single source of truth, loaded once |

**Current approach is recommended because:**
- Models don't change at runtime
- Consistent pricing across requests
- Simple access from anywhere
- Loaded at startup

## Test Coverage

### Unit Tests

| Test Suite | Coverage |
|------------|----------|
| `TestRegistry_LoadFromYAML` | YAML loading |
| `TestRegistry_GetModel` | Model lookup |
| `TestRegistry_GetCost` | Cost calculation |
| `TestRegistry_GetModelsByProvider` | Provider filtering |
| `TestRegistry_GetModelsByTier` | Tier filtering |

**Running:**
```bash
make test ./internal/llm-proxy/models
```

## Dependencies and Location

| Package | Purpose |
|---------|---------|
| `gopkg.in/yaml.v3` | YAML parsing |

**File Locations:**
- [`internal/llm-proxy/models/registry.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/llm-proxy/models/registry.go)
- [`internal/llm-proxy/models/types.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/llm-proxy/models/types.go)
- [`internal/llm-proxy/models/costs.go`](https://github.com/nevaria-ai/gateway/blob/main/internal/llm-proxy/models/costs.go)

## Related

- [Config Models](../../../config/models.md) - Configuration source
- [LLM Proxy README](README.md) - Main proxy documentation
