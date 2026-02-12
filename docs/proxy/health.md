# Provider Health Tracking

Circuit breaker pattern for LLM provider availability.

## States

```
Healthy ──▶ Degraded ──▶ Unhealthy ──▶ Recovering ──▶ Healthy
   ▲                                                        │
   └────────────────────────────────────────────────────────┘
```

## Thresholds

| Metric | Healthy | Degraded | Unhealthy |
|---------|----------|------------|------------|
| **Success rate** | >95% | 80-95% | <80% |
| **Latency** | <2s | 2-5s | >5s |
| **Consecutive failures** | <3 | 3-5 | >5 |

## Circuit Breaker

```
              ┌──────────────┐
Request ─────▶│ Circuit State │────▶ Provider
              └──────────────┘
                 │
                 ▼
         ┌─────────────────┐
         │ Open/Closed?   │
         └─────────────────┘
```

**States:**
- **Closed** - Normal operation, requests pass through
- **Open** - Provider failing, requests rejected early
- **Half-Open** - Testing recovery with limited requests

## Operations

### RecordSuccess

```go
health.RecordSuccess(provider, model, latency)
// Decrements failure count, updates success rate
```

### RecordFailure

```go
health.RecordFailure(provider, model, err)
// Increments failure count, may open circuit
```

### IsHealthy

```go
if !health.IsHealthy(provider, model) {
    return SkipProvider
}
```

## Auto-Recovery

After cooldown period, circuit enters "half-open":
1. Allow single test request
2. If success → close circuit (恢复正常)
3. If failure → open circuit again (延长冷却)

## Providers Monitored

- **OpenAI** - GPT-4o, GPT-4o-mini, o1-preview, o1-mini
- **Anthropic** - Claude 3.5 Sonnet, Haiku, Opus
- **Google** - Gemini 2.0 Flash, Gemini 2.5 Pro
