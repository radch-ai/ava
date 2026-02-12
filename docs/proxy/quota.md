# Quota Management

The quota system enforces spending limits and manages user credits.

## Core Concepts

```
Monthly Quota ──────▶ Reserved Quota ──────▶ Deducted Quota
                                                  │
                                                  ▼
                                          Remaining Available
```

## Operations

### CheckQuota

Verifies if user has sufficient quota for an operation.

```go
available, err := quota.CheckQuota(userID, estimatedCost)
if err != nil {
    return QuotaExceeded
}
```

### ReserveQuota

Reserves quota for an in-flight request. Prevents overspending when concurrent requests occur.

```go
reserved, err := quota.ReserveQuota(userID, estimatedCost)
if err != nil {
    return QuotaExceeded
}
// Request proceeds...
// Later: quota.DeductQuota(userID, actualCost)
```

### DeductQuota

Finalizes actual cost after request completes. Uses reserved amount if available.

```go
quota.DeductQuota(userID, actualCost, reservedID)
```

## Limits

| Limit Type | Default | Purpose |
|------------|----------|---------|
| **Monthly** | $20 (base), $100 (premium) | Credit plan limit |
| **6-Hourly** | 25% of monthly | Prevents rapid depletion |

## Plans

| Plan | Monthly | Features |
|-------|---------|-----------|
| **Base** | $20 | Economy + Premium tiers |
| **Premium** | $100 | Economy + Premium + Ultra tiers |

## Implementation

Current: In-memory mock for testing.
Planned: PostgreSQL backend with atomic operations.
