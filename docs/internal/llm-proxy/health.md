# internal/llm-proxy/health (internal/llm-proxy/health/)

Circuit breaker pattern for provider health monitoring.

## Overview

```mermaid
flowchart TB
    Start(["IsHealthy(provider, model)"]) --> CheckCircuit{circuit open?}
    CheckCircuit -->|yes| CheckTimeout{timeout passed?}
    CheckTimeout -->|no| Unhealthy(["return false"])
    CheckTimeout -->|yes| HalfOpen["half-open state"]
    HalfOpen --> Healthy(["return true"])
    CheckCircuit -->|no| CheckFailures{consecutive failures?}
    CheckFailures -->|>= 5| OpenCircuit["open circuit"]
    CheckFailures -->|< 5| Healthy

    Success(["RecordSuccess()"]) --> CloseCircuit["close circuit"]
    Failure(["RecordFailure()"]) --> IncFailures["increment failures"]
    IncFailures --> CheckThreshold{>= 5?}
    CheckThreshold -->|yes| OpenCircuit

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Success,Failure mainFn
    class CheckCircuit,CheckTimeout,HalfOpen,CheckFailures,OpenCircuit,CloseCircuit,IncFailures,CheckThreshold internalFn
    class Unhealthy errorFlow
```

## Structs

### CircuitBreaker

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `CircuitBreaker` | `failures` | `int` | Consecutive failures |
| | `lastFailure` | `time.Time` | Last failure time |
| | `state` | `CircuitState` | Current state |
| | `openUntil` | `time.Time` | Circuit open timeout |

### RedisHealthStore

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RedisHealthStore` | `redis` | `*redis.Client` | Redis client |

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewCircuitBreaker` | `(opts CircuitBreakerOptions) *CircuitBreaker` | Creates circuit breaker |
| `IsHealthy` | `(ctx context.Context, provider, model string) (bool, error)` | Checks health |
| `RecordSuccess` | `(ctx context.Context, provider, model string) error` | Records success |
| `RecordFailure` | `(ctx context.Context, provider, model string) error` | Records failure |

## Circuit States

| State | Description |
|-------|-------------|
| `Closed` | Normal operation |
| `Open` | Failing, reject requests |
| `HalfOpen` | Testing recovery |

## Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| Failure threshold | 5 | Failures before opening |
| Timeout | 30s | Time before half-open |

## Related

- [LLM Proxy README](README.md) - Main proxy documentation
