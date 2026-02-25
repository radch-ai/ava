# LLM Proxy Health

Circuit breaker pattern for provider health monitoring.

## Overview

```mermaid
flowchart TB
    Start(["IsHealthy(ctx, provider, model)"]) --> HGetHealth

    subgraph SGGetHealth [" "]
        HGetHealth["**GetHealth(ctx, client, provider, model) → health, err**"]
        HGetHealth -->|error| E1["return false, err"]
        HGetHealth --> HCheckCircuit
    end

    subgraph SGCheckCircuit [" "]
        HCheckCircuit["**!health.CircuitOpenUntil.IsZero() && time.Now().Before(health.CircuitOpenUntil)**"]
        HCheckCircuit -->|true| E2["return false, nil"]
        HCheckCircuit --> HCheckStatus
    end

    subgraph SGCheckStatus [" "]
        HCheckStatus["**health.Status == StatusHealthy**"]
        HCheckStatus --> Done(["return healthy, nil"])
    end

    StartSuccess(["RecordSuccess(ctx, provider, model)"]) --> HGetSuccess

    subgraph SGGetSuccess [" "]
        HGetSuccess["**store.Get(ctx, provider, model) → health, err**"]
        HGetSuccess -->|error| E3["return err"]
        HGetSuccess --> HUpdateSuccess
    end

    subgraph SGUpdateSuccess [" "]
        HUpdateSuccess["**health.SuccessCount++**"]
        HUpdateSuccess --> HSetLastSuccess
    end

    subgraph SGSetLastSuccess [" "]
        HSetLastSuccess["**health.LastSuccess = time.Now()**"]
        HSetLastSuccess --> HResetFailures
    end

    subgraph SGResetFailures [" "]
        HResetFailures["**health.ConsecutiveFailures = 0**"]
        HResetFailures --> HCheckStatusSuccess
    end

    subgraph SGCheckStatusSuccess [" "]
        HCheckStatusSuccess["**health.Status == StatusUnhealthy || health.Status == StatusDegraded**"]
        HCheckStatusSuccess -->|true| HSetHealthy
    end

    subgraph SGSetHealthy [" "]
        HSetHealthy["**health.Status = StatusHealthy**"]
        HSetHealthy --> HClearCircuit
    end

    subgraph SGClearCircuit [" "]
        HClearCircuit["**health.CircuitOpenUntil = time.Time{}**"]
        HClearCircuit --> HSaveSuccess
    end

    subgraph SGSaveSuccess [" "]
        HSaveSuccess["**store.Save(ctx, provider, model, health) → err**"]
        HSaveSuccess --> DoneSuccess(["return err"])
    end

    HCheckStatusSuccess --> HSaveSuccess

    StartFailure(["RecordFailure(ctx, provider, model)"]) --> HGetFailure

    subgraph SGGetFailure [" "]
        HGetFailure["**store.Get(ctx, provider, model) → health, err**"]
        HGetFailure -->|error| E4["return err"]
        HGetFailure --> HGetConfig
    end

    subgraph SGGetConfig [" "]
        HGetConfig["**config := DefaultCircuitBreakerConfig()**"]
        HGetConfig --> HUpdateFailure
    end

    subgraph SGUpdateFailure [" "]
        HUpdateFailure["**health.FailureCount++**"]
        HUpdateFailure --> HIncConsecutive
    end

    subgraph SGIncConsecutive [" "]
        HIncConsecutive["**health.ConsecutiveFailures++**"]
        HIncConsecutive --> HSetLastFailure
    end

    subgraph SGSetLastFailure [" "]
        HSetLastFailure["**health.LastFailure = time.Now()**"]
        HSetLastFailure --> HSwitchStatus
    end

    subgraph SGSwitchStatus [" "]
        HSwitchStatus["**switch health.ConsecutiveFailures >= config.FailureThreshold**"]
        HSwitchStatus -->|>= FailureThreshold| HSetUnhealthy
    end

    subgraph SGSetUnhealthy [" "]
        HSetUnhealthy["**health.Status = StatusUnhealthy**"]
        HSetUnhealthy --> HSetCircuitOpen
    end

    subgraph SGSetCircuitOpen [" "]
        HSetCircuitOpen["**health.CircuitOpenUntil = time.Now().Add(config.RecoveryTimeout)**"]
        HSetCircuitOpen --> HSaveFailure
    end

    HSwitchStatus -->|>= DegradedThreshold| HSetDegraded
    HSwitchStatus -->|< DegradedThreshold| HSetDefaultHealthy

    subgraph SGSetDegraded [" "]
        HSetDegraded["**health.Status = StatusDegraded**"]
        HSetDegraded --> HSaveFailure
    end

    subgraph SGSetDefaultHealthy [" "]
        HSetDefaultHealthy["**health.Status = StatusHealthy**"]
        HSetDefaultHealthy --> HSaveFailure
    end

    subgraph SGSaveFailure [" "]
        HSaveFailure["**store.Save(ctx, provider, model, health) → err**"]
        HSaveFailure --> DoneFailure(["return err"])
    end

    GetHealth["store.Get(ctx, provider, model) → health, err"]
    SaveHealth["store.Save(ctx, provider, model, health) → err"]
    Now["time.Now()"]
    AddTimeout["time.Now().Add(config.RecoveryTimeout)"]

    click GetHealth href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Get"
    click SaveHealth href "https://pkg.go.dev/github.com/redis/go-redis/v9#Client.Set"
    click Now href "https://pkg.go.dev/time#Now"
    click AddTimeout href "https://pkg.go.dev/time#Time.Add"

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef localPkgs fill:#fff8e1,stroke:#f9a825,stroke-width:1.5px,stroke-dasharray:4 2
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Done,StartSuccess,DoneSuccess,StartFailure,DoneFailure mainFn
    class GetHealth,SaveHealth,Now,AddTimeout localPkgs
    class E1,E2,E3,E4 errorFlow
```

## Structs

### Service

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Service` | `store` | `*Store` | Health store |

### Store

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `Store` | `client` | `*redis.Client` | Redis client |
| | `config` | `*CircuitBreakerConfig` | Circuit breaker config |

### ProviderHealth

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `ProviderHealth` | `Status` | `HealthStatus` | Health status |
| | `FailureCount` | `int` | Total failures |
| | `SuccessCount` | `int` | Total successes |
| | `LastFailure` | `time.Time` | Last failure time |
| | `LastSuccess` | `time.Time` | Last success time |
| | `ConsecutiveFailures` | `int` | Consecutive failures |
| | `CircuitOpenUntil` | `time.Time` | Circuit open timeout |

### CircuitBreakerConfig

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `CircuitBreakerConfig` | `FailureThreshold` | `int` | Failures to trip circuit |
| | `RecoveryTimeout` | `time.Duration` | Time before recovery attempt |
| | `SuccessThreshold` | `int` | Successes to close circuit |
| | `DegradedThreshold` | `int` | Failures to mark degraded |

## HealthStatus

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `StatusHealthy` | `HealthStatus` | `"healthy"` | Normal operation |
| `StatusDegraded` | `HealthStatus` | `"degraded"` | Some failures |
| `StatusUnhealthy` | `HealthStatus` | `"unhealthy"` | Circuit open |

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewService` | `(client *redis.Client, config *CircuitBreakerConfig) *Service` | Creates health service |
| `GetHealth` | `(ctx context.Context, client *redis.Client, provider, model string) (*ProviderHealth, error)` | Gets health data |
| `IsHealthy` | `(ctx context.Context, client *redis.Client, provider, model string) (bool, error)` | Checks if healthy |
| `RecordSuccess` | `(ctx context.Context, client *redis.Client, provider, model string) error` | Records success |
| `RecordFailure` | `(ctx context.Context, client *redis.Client, provider, model string) error` | Records failure |
| `GetAllHealth` | `(ctx context.Context, client *redis.Client) (map[string]*ProviderHealth, error)` | Gets all health data |
| `ResetHealth` | `(ctx context.Context, client *redis.Client, provider, model string) error` | Resets health |
| `GetUnhealthyModels` | `(ctx context.Context, client *redis.Client) ([]string, error)` | Gets unhealthy models |
| `GetDegradedModels` | `(ctx context.Context, client *redis.Client) ([]string, error)` | Gets degraded models |
| `ShouldRateLimit` | `(ctx context.Context, client *redis.Client, provider, model string) (bool, error)` | Checks if should rate limit |
| `DefaultCircuitBreakerConfig` | `() *CircuitBreakerConfig` | Returns default config |

## Functions (store.go)

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewStore` | `(client *redis.Client, config *CircuitBreakerConfig) *Store` | Creates store |
| `Get` | `(ctx context.Context, provider, model string) (*ProviderHealth, error)` | Gets health data |
| `Save` | `(ctx context.Context, provider, model string, health *ProviderHealth) error` | Saves health data |
| `Delete` | `(ctx context.Context, provider, model string) error` | Deletes health data |
| `GetAll` | `(ctx context.Context) (map[string]*ProviderHealth, error)` | Gets all health data |

## Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| FailureThreshold | 3 | Failures before opening circuit |
| RecoveryTimeout | 60s | Time before half-open |
| SuccessThreshold | 2 | Successes to close circuit |
| DegradedThreshold | 2 | Failures to mark degraded |

## Redis Keys

| Key Pattern | Description |
|-------------|-------------|
| `health:{provider}:{model}` | Health data for provider:model |

Health data is stored as JSON with 24-hour expiration.

## Related

- [LLM Proxy README](README.md) - Main proxy documentation
