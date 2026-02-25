# LLM Proxy Router

LLM-based model selection router.

## Overview

```mermaid
flowchart TB
    Start(["SelectModel(ctx, llmClient, req)"]) --> HBuildPrompt

    subgraph SGBuildPrompt [" "]
        HBuildPrompt["**BuildRouterPrompt(req) → prompt**"]
        HBuildPrompt --> HMkMessages
    end

    subgraph SGMkMessages [" "]
        HMkMessages["**messages := []AgentMessage{NewTextMessage(RoleUser, prompt)}**"]
        HMkMessages --> HStream
    end

    HStream --> HLLMStream

    subgraph SGLLMStream [" "]
        HLLMStream["**llmClient.Stream(ctx, messages, []Tool{}) → eventCh, err**"]
        HLLMStream -->|error| E1["return err"]
        HLLMStream --> HCollect
    end

    subgraph SGCollect [" "]
        HCollect["**for event := range eventCh**"]
        HCollect --> HCheckEvent
    end

    subgraph SGCheckEvent [" "]
        HCheckEvent["**switch e := event.(type)**"]
        HCheckEvent -->|MessageEndEvent| HSetMsg["assistantMsg = &e.Message"]
        HCheckEvent -->|other| HCollect
        HSetMsg --> HCollect
    end

    HCollect --> HNilCheck

    subgraph SGNilCheck [" "]
        HNilCheck["**assistantMsg == nil**"]
        HNilCheck -->|true| E2["return error"]
        HNilCheck --> HExtractJSON
    end

    subgraph SGExtractJSON [" "]
        HExtractJSON["**for _, block := range assistantMsg.Content**"]
        HExtractJSON --> HTextCheck
    end

    subgraph SGTextCheck [" "]
        HTextCheck["**tc, ok := block.(TextContent); ok**"]
        HTextCheck -->|true| HSetJSON["jsonText = tc.Text"]
        HTextCheck -->|false| HExtractJSON
        HSetJSON --> HExtractJSON
    end

    HExtractJSON --> HUnmarshal

    subgraph SGUnmarshal [" "]
        HUnmarshal["**json.Unmarshal([]byte(jsonText), &resp) → err**"]
        HUnmarshal -->|error| E3["return err"]
        HUnmarshal --> Done(["return &resp, nil"])
    end

    NewTextMessage["agent.NewTextMessage(agent.RoleUser, prompt) → AgentMessage"]

    click NewTextMessage href "../agent/README.md"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Done mainFn
    class NewTextMessage localPkgs
    class E1,E2,E3 errorFlow
```

## Structs

### RouterRequest

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RouterRequest` | `UserQuery` | `string` | User query |
| | `AvailableModels` | `[]string` | Available models |
| | `UserContext` | `UserContext` | User context |
| | `ModelCosts` | `map[string]CostInfo` | Model costs |
| | `PreviousAttempts` | `[]RouterAttempt` | Previous failed attempts |
| | `UnhealthyModels` | `[]string` | Unhealthy models |

### RouterResponse

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RouterResponse` | `SelectedModel` | `string` | Selected model |
| | `Reasoning` | `string` | Selection reasoning |
| | `Confidence` | `string` | Confidence level |
| | `FallbackOptions` | `[]string` | Fallback models |
| | `EstimatedTokens` | `TokenEstimate` | Token estimate |
| | `EstimatedCost` | `CostEstimate` | Cost estimate |

### RouterAttempt

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RouterAttempt` | `Model` | `string` | Model attempted |
| | `Error` | `string` | Error message |

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `SelectModel` | `(ctx context.Context, llmClient LLMClient, req RouterRequest) (*RouterResponse, error)` | Selects best model |
| `BuildRouterPrompt` | `(req RouterRequest) string` | Builds router prompt |

## Router Prompt Sections

The `BuildRouterPrompt` function creates a structured prompt with these sections:

| Section | Content |
|---------|---------|
| User Query | The user's query text |
| User Context | Plan tier, quota remaining, addon credits, days until reset |
| Available Models | List of models with costs per million tokens |
| Previous Attempts | List of failed models with errors (if any) |
| Unhealthy Models | List of unhealthy models to avoid (if any) |
| Instructions | Selection criteria and response format |

## Response Format

The router model responds with JSON in this format:

```json
{
  "selected_model": "model-name",
  "reasoning": "brief explanation",
  "confidence": "high|medium|low",
  "fallback_options": ["model1", "model2"],
  "estimated_tokens": {"input": 100, "output": 50},
  "estimated_cost": {"input": 0.0001, "output": 0.0002, "total": 0.0003}
}
```

## Related

- [LLM Proxy README](README.md) - Main proxy documentation
- [Models](models.md) - Model registry
- [Agent](../agent/README.md) - Agent message types
