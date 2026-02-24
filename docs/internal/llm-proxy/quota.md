# LLM Proxy Quota

Quota management with Redis backend and plan/addon credit tracking.

## Overview

```mermaid
flowchart TB
    Start("CheckQuota(ctx, userID, cost)") --> GetPlan["GetPlan(userID)"]
    GetPlan --> CheckPlan["check plan credits"]
    CheckPlan -->|sufficient| ReturnPlan("return plan source")
    CheckPlan -->|insufficient| CheckAddon["check addon credits"]
    CheckAddon -->|has addon| ReturnAddon("return addon source")
    CheckAddon -->|no addon| NoQuota("return HasQuota=false")

    Reserve("ReserveQuota()") --> DeductRedis["deduct from Redis"]
    Deduct("DeductQuota()") --> UpdateDB["update database"]

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Reserve,Deduct,ReturnPlan,ReturnAddon mainFn
    class GetPlan,CheckPlan,CheckAddon,DeductRedis,UpdateDB internalFn
    class NoQuota errorFlow
```

## Structs

### RedisQuotaChecker

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RedisQuotaChecker` | `redis` | `*redis.Client` | Redis client |
| | `logger` | `zerolog.Logger` | Logger |

### QuotaCheckResult

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `QuotaCheckResult` | `HasQuota` | `bool` | Has sufficient quota |
| | `Source` | `string` | "plan" or "addon" |
| | `PlanPercent` | `int` | Plan usage percent |
| | `PlanDollars` | `float64` | Plan dollars remaining |
| | `AddonDollars` | `float64` | Addon dollars remaining |
| | `UsingAddon` | `bool` | Using addon credits |
| | `UserContext` | `UserContext` | Full user context |

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewRedisQuotaChecker` | `(redis *redis.Client, logger zerolog.Logger) *RedisQuotaChecker` | Creates checker |
| `CheckQuota` | `(ctx context.Context, userID string, estimatedCost float64) (*QuotaCheckResult, error)` | Checks quota |
| `ReserveQuota` | `(ctx context.Context, userID string, cost float64, source string) error` | Reserves credits |
| `DeductQuota` | `(ctx context.Context, userID string, reserved, actual float64, source string) error` | Deducts credits |

## Redis Keys

| Key Pattern | Description |
|-------------|-------------|
| `quota:{userID}:plan` | Plan credit balance |
| `quota:{userID}:addon` | Addon credit balance |
| `quota:{userID}:period` | Period usage |

## Related

- [LLM Proxy README](README.md) - Main proxy documentation
- [Config Plans](../../../config/plans.md) - Plan definitions
