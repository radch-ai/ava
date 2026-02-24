# LLM Proxy Usage

Usage tracking with Redis primary and file fallback.

## Overview

```mermaid
flowchart TB
    Start(["Track(ctx, event)"]) --> TryRedis["try Redis.RPush()"]
    TryRedis -->|success| Done(["return nil"])
    TryRedis -->|error| Fallback["append to file"]
    Fallback -->|success| Done
    Fallback -->|error| Err(["return error"])

    classDef mainFn fill:#e3f2fd,stroke:#1565c0,stroke-width:2px
    classDef internalFn fill:#f3e5f5,stroke:#6a1b9a,stroke-width:2px,stroke-dasharray:6 3
    classDef errorFlow fill:#ffebee,stroke:#c62828,stroke-width:1px

    class Start,Done mainFn
    class TryRedis,Fallback internalFn
    class Err errorFlow
```

## Structs

### RedisUsageTracker

| Struct | Field | Type | Description |
|--------|-------|------|-------------|
| `RedisUsageTracker` | `redis` | `*redis.Client` | Redis client |
| | `fallbackPath` | `string` | Fallback file path |

## Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `NewRedisUsageTracker` | `(redis *redis.Client, fallbackPath string) *RedisUsageTracker` | Creates tracker |
| `Track` | `(ctx context.Context, event UsageEvent) error` | Records usage event |

## Redis Keys

| Key Pattern | Description |
|-------------|-------------|
| `usage:{userID}:events` | Usage event list |
| `usage:{userID}:daily:{date}` | Daily summary |

## Fallback Behavior

| Condition | Behavior |
|-----------|----------|
| Redis available | Append to Redis list |
| Redis unavailable | Append to local file |
| Both fail | Return error |

## Related

- [LLM Proxy README](README.md) - Main proxy documentation
