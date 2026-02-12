# internal/proxy/router

LLM-based intelligent model selection.

## Description

The router package uses an LLM to intelligently select the best model for each request based on query complexity, user quota, and costs.

## Router Selection Flow

```mermaid
flowchart TB
    Start["SelectModel"] --> Build["Build router prompt"]
    Build --> PromptContent["User query - User context - Available models - Model costs"]
    PromptContent --> Call["Call router LLM - cheap, fast"]
    Call --> Stream["Stream response - Extract JSON"]
    Stream --> Parse["Parse RouterResponse"]
    Parse --> Return["Return selected model + reasoning"]
```

## Prompt Structure

```mermaid
flowchart TB
    RouterPrompt["Router Prompt"] --> UserQ["1. USER QUERY - The user's actual input text"]
    RouterPrompt --> UserC["2. USER CONTEXT - Plan tier - Plan quota remaining - Addon quota remaining - Days until reset"]
    RouterPrompt --> Models["3. AVAILABLE MODELS - List of models - Input/output cost per million"]
    RouterPrompt --> Prev["4. PREVIOUS ATTEMPTS - Failed model selections with reasons"]
    RouterPrompt --> Unhealthy["5. UNHEALTHY MODELS - Models to avoid due to health issues"]
    RouterPrompt --> Instr["6. INSTRUCTIONS - Select best model - Consider cost efficiency - Respect quota - Return JSON"]
```

## Types and Functions

| Name | Kind | Location | Description |
|------|------|----------|-------------|
| RouterRequest | interface | [types.go#L17](../internal/proxy/router/types.go#L17) | Router request data |
| RouterResponse | struct | [types.go#L28](../internal/proxy/router/types.go#L28) | Router response data |
| RouterAttempt | struct | [types.go#L38](../internal/proxy/router/types.go#L38) | Failed attempt tracking |
| SelectModel | func | [router.go#L17](../internal/proxy/router/router.go#L17) | Calls router LLM |
| BuildRouterPrompt | func | [prompt.go#L9](../internal/proxy/router/prompt.go#L9) | Creates router prompt |
| LLMClient | interface | [router.go#L12](../internal/proxy/router/router.go#L12) | LLM client interface |

## RouterRequest Fields

| Field | Type | Description |
|-------|------|-------------|
| `UserQuery` | string | The user's query text |
| `AvailableModels` | []string | Models user can access |
| `UserContext` | UserContext | Quota and plan info |
| `ModelCosts` | map[string]CostInfo | Pricing for each model |
| `PreviousAttempts` | []RouterAttempt | Failed attempts |
| `UnhealthyModels` | []string | Models to avoid |

## RouterResponse Fields

| Field | Type | Description |
|-------|------|-------------|
| `SelectedModel` | string | Chosen model |
| `Reasoning` | string | Explanation of choice |
| `Confidence` | string | high/medium/low |
| `FallbackOptions` | []string | Alternative models |
| `EstimatedTokens` | TokenEstimate | Input/output tokens |
| `EstimatedCost` | CostEstimate | Input/output/total cost |

## Selection Criteria

- Query complexity (simple -> economy, complex -> premium)
- User's plan tier and remaining quota
- Cost efficiency
- Model health status
- Previous failures
