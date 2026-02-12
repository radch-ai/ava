# LLM Proxy

The LLM proxy sits between the agent and LLM API providers, providing intelligent routing, quota management, and usage tracking.

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────────┐
│   Agent     │────▶│    Proxy      │────▶│  LLM Provider  │
│  (internal) │     │  (internal)   │     │  (external)     │
└─────────────┘     └──────────────┘     └─────────────────┘
                           │
                           │
                           ▼
                    ┌──────────────┐
                    │   Router      │
                    │  (decides)   │
                    └──────────────┘
```

## Components

| Component | Purpose |
|-----------|---------|
| **types** | Core data structures (UsageEvent, UserContext, ProviderHealth) |
| **models** | Model registry with YAML configuration, tier-based access |
| **quota** | Quota checking, reservation, deduction with plan limits |
| **health** | Provider health tracking with circuit breaker pattern |
| **router** | Intelligent model selection using cheap LLM for decision-making |
| **usage** | Multi-layer usage tracking (DB → Redis → Disk) |
| **middleware** | Service pause mechanism to prevent data loss |
| **handler** | Main orchestrator combining all services |
| **client** | agent.LLMClient implementation wrapping the handler |

## Flow

1. Agent calls LLM via `proxy.Client`
2. Handler checks quota and reserves tokens
3. Router selects appropriate model based on budget/context
4. Provider health verified before call
5. LLM provider invoked
6. Usage tracked asynchronously
7. Response returned to agent

## Model Tiers

| Tier | Description | Models |
|-------|-------------|---------|
| **economy** | Lowest cost, basic quality | gpt-4o-mini, claude-3-5-haiku, gemini-2.0-flash |
| **premium** | Balanced cost/quality | gpt-4o, claude-3-5-sonnet, gemini-2.5-pro |
| **ultra** | Highest quality, highest cost | o1-preview, claude-3-opus |

## Configuration

Models are configured via `config/models.yaml`:

```yaml
models:
  gpt-4o:
    provider: openai
    tier: premium
    input_cost_per_million: 2.50
    output_cost_per_million: 10.00
    max_tokens: 128000

router:
  model: gpt-4o-mini
  timeout: 5s
```

## Routing Strategy

The router uses a cheap LLM to make model selection decisions based on:

1. **User's plan** - $20 vs $100 plans have different model access
2. **Quota remaining** - Monthly and 6-hourly limits
3. **Query complexity** - Simple queries use economy, complex use premium/ultra
4. **Quality requirements** - Explicit user preferences

## Services

All proxy services support two modes:

- **Mock mode** (current) - In-memory for testing
- **Real mode** (pending) - PostgreSQL, Redis, actual LLM clients

See `.claude/plans/2025-02-12-llm-proxy.md` Task 13 for details.
