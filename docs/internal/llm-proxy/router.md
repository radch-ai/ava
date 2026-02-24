# LLM Proxy Router

LLM-based model selection router.

## Overview

```mermaid
flowchart TB
    Start("SelectModel(ctx, request)") --> GetModels["GetAvailableModels()"]
    GetModels --> BuildPrompt["build router prompt"]
    BuildPrompt --> CallLLM["call router LLM"]
    CallLLM --> Parse["parse model selection"]
    Parse --> Estimate["estimate tokens & cost"]
    Estimate --> Response("return RouterResponse")

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3

    class Start,Response mainFn
    class GetModels,BuildPrompt,CallLLM,Parse,Estimate internalFn
```

## Structs

### LLMRouter

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `LLMRouter` | `llmClient` | `LLMClient` | LLM client for routing |
| | `modelRegistry` | `*models.Registry` | Model registry |
| | `routerModel` | `string` | Model for routing |

### RouterResponse

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RouterResponse` | `SelectedModel` | `string` | Selected model |
| | `EstimatedTokens` | `TokenEstimate` | Token estimate |
| | `EstimatedCost` | `CostEstimate` | Cost estimate |
| | `FallbackOptions` | `[]string` | Fallback models |

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewLLMRouter` | `(opts RouterOptions) *LLMRouter` | Creates router |
| `SelectModel` | `(ctx context.Context, req RouterRequest) (*RouterResponse, error)` | Selects best model |

## Related

- [LLM Proxy README](README.md) - Main proxy documentation
- [Models](models.md) - Model registry
