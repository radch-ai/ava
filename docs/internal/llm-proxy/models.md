# LLM Proxy Models

Model registry with cost calculation and metadata.

### NewRegistry

```mermaid
flowchart TB
    Start(["NewRegistry()"]) --> HNew

    subgraph SGNew [" "]
        HNew["**models := make(map[string]*ModelConfig)**"]
        HNew --> Return(["return &Registry{models: models}"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### Get

```mermaid
flowchart TB
    Start(["Get()"]) --> HOnce

    subgraph SGOnce [" "]
        HOnce["**sync.Once.Do(func)**"]
        HOnce --> HCallNew
    end

    subgraph SGCallNew [" "]
        HCallNew["**globalRegistry = NewRegistry()**"]
        HCallNew --> Return(["return globalRegistry"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### LoadFromYAML

```mermaid
flowchart TB
    Start(["LoadFromYAML(path)"]) --> HReadFile

    subgraph SGReadFile [" "]
        HReadFile["**os.ReadFile(path) → data, err**"]
        HReadFile -->|error| E1["return err"]
        HReadFile --> HUnmarshal
    end

    subgraph SGUnmarshal [" "]
        HUnmarshal["**yaml.Unmarshal(data, &config) → err**"]
        HUnmarshal -->|error| E2["return err"]
        HUnmarshal --> HLock
    end

    subgraph SGLock [" "]
        HLock["**r.mu.Lock()**"]
        HLock --> HStoreModels
    end

    subgraph SGStoreModels [" "]
        HStoreModels["**for name, cfg := range config.Models**"]
        HStoreModels --> HSetName["**cfg.Name = name**"]
        HSetName --> HMapStore["**r.models[name] = cfg**"]
        HMapStore --> HUnlock
    end

    subgraph SGUnlock [" "]
        HUnlock["**r.mu.Unlock()**"]
        HUnlock --> HStoreRouter
    end

    subgraph SGStoreRouter [" "]
        HStoreRouter["**r.router = config.Router**"]
        HStoreRouter --> Return(["return nil"])
    end

    ReadFile["os.ReadFile(path) → data, err"]
    Unmarshal["yaml.Unmarshal(data, &config) → err"]

    click ReadFile href "https://pkg.go.dev/os#ReadFile"
    click Unmarshal href "https://pkg.go.dev/gopkg.in/yaml.v3#Unmarshal"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Return mainFn
    class ReadFile,Unmarshal localPkgs
    class E1,E2 errorFlow
```

### GetModel

```mermaid
flowchart TB
    Start(["GetModel(name)"]) --> HLock

    subgraph SGLock [" "]
        HLock["**r.mu.RLock()**"]
        HLock --> HLookup
    end

    subgraph SGLookup [" "]
        HLookup["**cfg, exists := r.models[name]**"]
        HLookup --> HUnlock
    end

    subgraph SGUnlock [" "]
        HUnlock["**r.mu.RUnlock()**"]
        HUnlock --> Return(["return cfg, exists"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### GetRouterModel

```mermaid
flowchart TB
    Start(["GetRouterModel()"]) --> HLock

    subgraph SGLock [" "]
        HLock["**r.mu.RLock()**"]
        HLock --> HGetModel
    end

    subgraph SGGetModel [" "]
        HGetModel["**model := r.router.Model**"]
        HGetModel --> HUnlock
    end

    subgraph SGUnlock [" "]
        HUnlock["**r.mu.RUnlock()**"]
        HUnlock --> Return(["return model"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### GetAllModels

```mermaid
flowchart TB
    Start(["GetAllModels()"]) --> HLock

    subgraph SGLock [" "]
        HLock["**r.mu.RLock()**"]
        HLock --> HCreateSlice
    end

    subgraph SGCreateSlice [" "]
        HCreateSlice["**names := make([]string, 0, len(r.models))**"]
        HCreateSlice --> HIterate
    end

    subgraph SGIterate [" "]
        HIterate["**for name := range r.models**"]
        HIterate --> HAppend["**names = append(names, name)**"]
        HAppend --> HUnlock
    end

    subgraph SGUnlock [" "]
        HUnlock["**r.mu.RUnlock()**"]
        HUnlock --> Return(["return names"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### GetModelsByProvider

```mermaid
flowchart TB
    Start(["GetModelsByProvider(provider)"]) --> HLock

    subgraph SGLock [" "]
        HLock["**r.mu.RLock()**"]
        HLock --> HIterate
    end

    subgraph SGIterate [" "]
        HIterate["**for _, cfg := range r.models**"]
        HIterate --> HCheck["**cfg.Provider == provider**"]
        HCheck -->|true| HAppend["**models = append(models, cfg)**"]
        HAppend --> HIterate
        HCheck -->|false| HIterate
        HIterate --> HUnlock
    end

    subgraph SGUnlock [" "]
        HUnlock["**r.mu.RUnlock()**"]
        HUnlock --> Return(["return models"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### GetModelsByTier

```mermaid
flowchart TB
    Start(["GetModelsByTier(tier)"]) --> HLock

    subgraph SGLock [" "]
        HLock["**r.mu.RLock()**"]
        HLock --> HIterate
    end

    subgraph SGIterate [" "]
        HIterate["**for _, cfg := range r.models**"]
        HIterate --> HCheck["**cfg.Tier == tier**"]
        HCheck -->|true| HAppend["**models = append(models, cfg)**"]
        HAppend --> HIterate
        HCheck -->|false| HIterate
        HIterate --> HUnlock
    end

    subgraph SGUnlock [" "]
        HUnlock["**r.mu.RUnlock()**"]
        HUnlock --> Return(["return models"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### CalculateCost

```mermaid
flowchart TB
    Start(["CalculateCost(cfg, inputTokens, outputTokens)"]) --> HCalcInput

    subgraph SGCalcInput [" "]
        HCalcInput["**inputCost := float64(inputTokens) * cfg.InputCostPerMillion / 1000000**"]
        HCalcInput --> HCalcOutput
    end

    subgraph SGCalcOutput [" "]
        HCalcOutput["**outputCost := float64(outputTokens) * cfg.OutputCostPerMillion / 1000000**"]
        HCalcOutput --> Return(["return inputCost + outputCost"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
```

### EstimateCost

```mermaid
flowchart TB
    Start(["EstimateCost(cfg, estimate)"]) --> HCalcInput

    subgraph SGCalcInput [" "]
        HCalcInput["**inputCost := float64(estimate.Input) * cfg.InputCostPerMillion / 1000000**"]
        HCalcInput --> HCalcOutput
    end

    subgraph SGCalcOutput [" "]
        HCalcOutput["**outputCost := float64(estimate.Output) * cfg.OutputCostPerMillion / 1000000**"]
        HCalcOutput --> Return(["return &CostEstimate{Input: inputCost, Output: outputCost, Total: inputCost + outputCost}"])
    end

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px

    class Start,Return mainFn
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
